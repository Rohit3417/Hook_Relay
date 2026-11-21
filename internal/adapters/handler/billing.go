package handler

import (
	jwtauth "Hook_Relay2/internal/adapters/auth/jwtAuth"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type createOrderRequest struct {
	Tier string `json:"tier"` // "GROWTH" or "ENTERPRISE"
}

// 1. POST /api/v1/billing/create-order (Requires Bearer JWT)
func (h *Handler) CreateRazorpayOrder(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := jwtauth.GetTenantID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	// 1. Read the requested tier from client
	var reqBody createOrderRequest
	_ = json.NewDecoder(r.Body).Decode(&reqBody)
	targetTier := strings.ToUpper(reqBody.Tier)
	var amount int64
	// 2. Set price based on tier (in paise: ₹999 vs ₹4,999)
	switch targetTier {
	case "ENTERPRISE":
		amount = 499900 // ₹4,999
	case "GROWTH":
		amount = 99900 // ₹999
	default:
		writeError(w, http.StatusBadRequest, "invalid tier; must be 'GROWTH' or 'ENTERPRISE'")
		return
	}
	keyID := os.Getenv("RAZORPAY_KEY_ID")
	keySecret := os.Getenv("RAZORPAY_KEY_SECRET")
	// 3. Attach BOTH tenant_id AND target_tier inside notes
	orderReq := map[string]any{
		"amount":   amount,
		"currency": "INR",
		"receipt":  "rcpt_" + tenantID[:8],
		"notes": map[string]string{
			"tenant_id":   tenantID,
			"target_tier": targetTier, // Saved in Razorpay
		},
	}
	bodyBytes, _ := json.Marshal(orderReq)
	req, err := http.NewRequestWithContext(r.Context(), "POST", "https://api.razorpay.com/v1/orders", bytes.NewReader(bodyBytes))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build order request")
		return
	}
	req.SetBasicAuth(keyID, keySecret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "failed to create Razorpay order")
		return
	}
	defer resp.Body.Close()
	var orderResp map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&orderResp)
	writeJSON(w, http.StatusOK, map[string]any{
		"order_id": orderResp["id"],
		"amount":   orderResp["amount"],
		"currency": orderResp["currency"],
		"tier":     targetTier,
		"key_id":   keyID,
	})
}

// 2. POST /api/v1/billing/razorpay-webhook (Inbound Webhook from Razorpay)
func (h *Handler) HandleRazorpayWebhook(w http.ResponseWriter, r *http.Request) {
	// A) Read the raw request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	// B) Verify Razorpay's HMAC-SHA256 signature
	receivedSig := r.Header.Get("X-Razorpay-Signature")
	webhookSecret := os.Getenv("RAZORPAY_WEBHOOK_SECRET")

	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(body)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(expectedSig), []byte(receivedSig)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}

	// C) Parse the webhook JSON
	var webhookPayload struct {
		Event   string `json:"event"`
		Payload struct {
			Payment struct {
				Entity struct {
					Notes struct {
						TenantID   string `json:"tenant_id"`
						TargetTier string `json:"target_tier"` // Read from Razorpay
					} `json:"notes"`
				} `json:"entity"`
			} `json:"payment"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(body, &webhookPayload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload json")
		return
	}

	// D) Handle successful payment event
	notes := webhookPayload.Payload.Payment.Entity.Notes
	tenantID := notes.TenantID

	switch webhookPayload.Event {
	// A) SUCCESS: Upgrade tier
	case "payment.captured", "order.paid":
		targetTier := strings.ToUpper(notes.TargetTier)
		if targetTier != "GROWTH" && targetTier != "ENTERPRISE" {
			targetTier = "GROWTH"
		}

		if tenantID != "" {
			_, _ = h.pool.Exec(r.Context(), "UPDATE tenants SET tier = $1 WHERE id = $2", targetTier, tenantID)
			_ = h.client.Set(r.Context(), "tenant_tier:"+tenantID, targetTier, 24*time.Hour).Err()
			fmt.Printf("✅ [BILLING] Tenant %s upgraded to %s tier!\n", tenantID, targetTier)
		}

	// B) FAILURE: Downgrade back to STARTER
	case "payment.failed", "subscription.cancelled":
		if tenantID != "" {
			_, _ = h.pool.Exec(r.Context(), "UPDATE tenants SET tier = 'STARTER' WHERE id = $1", tenantID)
			_ = h.client.Set(r.Context(), "tenant_tier:"+tenantID, "STARTER", 24*time.Hour).Err()
			fmt.Printf("⚠️ [BILLING] Payment failed or cancelled. Tenant %s downgraded to STARTER tier.\n", tenantID)
		}
	}

	// Always return 200 OK to Razorpay
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
