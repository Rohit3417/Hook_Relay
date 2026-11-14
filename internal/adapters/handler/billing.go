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
)

// 1. POST /api/v1/billing/create-order (Requires Bearer JWT)
func (h *Handler) CreateRazorpayOrder(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := jwtauth.GetTenantID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	keyID := os.Getenv("RAZORPAY_KEY_ID")
	keySecret := os.Getenv("RAZORPAY_KEY_SECRET")

	// Order payload for ₹999 Pro Tier (amount is in paise: 999 * 100 = 99900)
	orderReq := map[string]any{
		"amount":   99900,
		"currency": "INR",
		"receipt":  "rcpt_" + tenantID[:8],
		"notes": map[string]string{
			"tenant_id": tenantID,
		},
	}
	bodyBytes, _ := json.Marshal(orderReq)

	// Call Razorpay API: POST https://api.razorpay.com/v1/orders
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

	// Return order details to frontend/client
	writeJSON(w, http.StatusOK, map[string]any{
		"order_id": orderResp["id"],
		"amount":   orderResp["amount"],
		"currency": orderResp["currency"],
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
						TenantID string `json:"tenant_id"`
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
	if webhookPayload.Event == "payment.captured" || webhookPayload.Event == "order.paid" {
		tenantID := webhookPayload.Payload.Payment.Entity.Notes.TenantID
		if tenantID != "" {
			// Upgrade tenant tier to PRO in Postgres!
			_, err := h.pool.Exec(r.Context(), "UPDATE tenants SET tier = 'PRO' WHERE id = $1", tenantID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update tenant tier")
				return
			}
			fmt.Printf(" [BILLING] Tenant %s successfully upgraded to PRO tier via Razorpay!\n", tenantID)
		}
	}

	// Always return 200 OK to Razorpay
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
