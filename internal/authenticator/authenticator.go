package authenticator

import "net/http"

type Authenticator interface {
	AuthenticateUser(h http.Handler) http.Handler
	RegisterNewUser(h http.Handler) http.Handler
}
