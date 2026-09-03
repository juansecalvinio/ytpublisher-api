package billing

import (
	"context"

	"github.com/stripe/stripe-go/v86"
)

type Client struct {
	sc *stripe.Client
}

func NewClient(secretKey string) *Client {
	return &Client{sc: stripe.NewClient(secretKey)}
}

func (c *Client) CreateCheckoutSession(ctx context.Context, priceID, successURL, cancelURL string) (string, error) {
	params := &stripe.CheckoutSessionCreateParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
			{Price: stripe.String(priceID)},
		},
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
	}
	session, err := c.sc.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return "", err
	}
	return session.URL, nil
}

const meterEventName = "ytpublisher_generate_request"

func (c *Client) ReportUsage(ctx context.Context, stripeCustomerID string) error {
	params := &stripe.BillingMeterEventCreateParams{
		EventName: stripe.String(meterEventName),
		Payload: map[string]string{
			"stripe_customer_id": stripeCustomerID,
			"value":              "1",
		},
	}
	_, err := c.sc.V1BillingMeterEvents.Create(ctx, params)
	return err
}
