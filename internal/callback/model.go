package callback

import "encoding/json"

type AuthCallback struct {
	Environment string
	Code        string
	UserID      int64
	Success     bool
	Payload     map[string]any
}

func (c AuthCallback) JSON() ([]byte, error) {
	return json.Marshal(c.Payload)
}

type Delivery string

const (
	DeliveryBackend       Delivery = "backend"
	DeliveryMySQLFallback Delivery = "mysql-fallback"
)
