package billing

import (
	"encoding/json"

	"github.com/stripe/stripe-go/v86"
)

type Event struct {
	Type string
	Data []byte
}

// ParseWebhookEvent verifies the Stripe-Signature header against secret and,
// only if valid, returns the event's type and raw object JSON.
//
// WithIgnoreAPIVersionMismatch is deliberate: we never deserialize into
// stripe-go's typed event objects (ParseCheckoutCompleted uses our own
// minimal struct instead), so a mismatch between the account's API version
// and the SDK's doesn't affect correctness here, and shouldn't fail webhook
// processing.
func ParseWebhookEvent(payload []byte, signatureHeader, secret string) (Event, error) {
	stripeEvent, err := stripe.ConstructEvent(payload, signatureHeader, secret, stripe.WithIgnoreAPIVersionMismatch())
	if err != nil {
		return Event{}, err
	}
	return Event{Type: string(stripeEvent.Type), Data: stripeEvent.Data.Raw}, nil
}

// CheckoutCompleted holds the fields we need from a checkout.session.completed
// event. It's parsed with our own struct rather than a stripe-go type so this
// stays correct regardless of how that SDK models the Checkout Session object.
type CheckoutCompleted struct {
	CustomerID string
	Email      string
	Name       string
}

func ParseCheckoutCompleted(data []byte) (CheckoutCompleted, error) {
	var raw struct {
		Customer        string `json:"customer"`
		CustomerDetails struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"customer_details"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return CheckoutCompleted{}, err
	}
	return CheckoutCompleted{
		CustomerID: raw.Customer,
		Email:      raw.CustomerDetails.Email,
		Name:       raw.CustomerDetails.Name,
	}, nil
}
