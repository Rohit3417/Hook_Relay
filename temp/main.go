package main

import (
	"fmt"
	"net/http"
)

var callCount = 0

func main() {
	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		fmt.Printf("call #%d, signature header: %s\n", callCount, r.Header.Get("Webhook-Signature"))
		if callCount < 52 {
			w.WriteHeader(http.StatusInternalServerError) // fail the first 2 calls
			return
		}
		w.WriteHeader(http.StatusOK) // succeed on the 3rd
	})
	http.ListenAndServe(":9091", nil)
}
