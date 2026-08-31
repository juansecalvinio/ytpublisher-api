package api

import (
	"context"
	"log"
	"net/http"
)

type CheckoutSessionCreator interface {
	CreateCheckoutSession(ctx context.Context, priceID, successURL, cancelURL string) (string, error)
}

func handleBillingSignup(creator CheckoutSessionCreator, priceID, successURL, cancelURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url, err := creator.CreateCheckoutSession(r.Context(), priceID, successURL, cancelURL)
		if err != nil {
			log.Printf("billing: failed to create checkout session: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to start checkout")
			return
		}
		http.Redirect(w, r, url, http.StatusSeeOther)
	}
}

func handleBillingSuccess(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Thanks for subscribing! Check your email for your API key.\n"))
}

func handleBillingCancel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Checkout canceled. No charge was made.\n"))
}
