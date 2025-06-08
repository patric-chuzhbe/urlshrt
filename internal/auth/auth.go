// Package auth provides middleware and helpers for JWT-based authentication
// and user identification in HTTP requests. It supports cookie-based or
// Authorization header-based token parsing.
package auth

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/golang-jwt/jwt/v4"
	"go.uber.org/zap"

	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
)

type userKeeper interface {
	CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (string, error)
	GetUserByID(ctx context.Context, userID string, transaction *sql.Tx) (*user.User, error)
}

// Auth handles user authentication and JWT token management.
// It supports retrieving user information and setting authorization cookies.
type Auth struct {
	// db is the interface to the user data storage.
	db userKeeper

	// authCookieName is the name of the cookie used to store the JWT.
	authCookieName string

	// authCookieSigningSecretKey is the key used to sign JWTs.
	authCookieSigningSecretKey []byte
}

// Claims represents the JWT claims used by the system.
// It embeds standard JWT claims and adds a user-specific identifier.
type Claims struct {
	jwt.RegisteredClaims
	UserID string `json:"user_id"`
}

// ContextKey is a custom type for storing values in context to avoid collisions.
type ContextKey string

// UserIDKey is the context key used to store and retrieve the authenticated user's ID.
const UserIDKey ContextKey = "userID"

// New creates a new Auth handler with the given user data access layer,
// cookie name, and JWT signing secret.
func New(
	db userKeeper,
	authCookieName string,
	authCookieSigningSecretKey []byte,
) *Auth {
	return &Auth{
		db:                         db,
		authCookieName:             authCookieName,
		authCookieSigningSecretKey: authCookieSigningSecretKey,
	}
}

// RegisterNewUser is an HTTP middleware that registers a new user if none exists
// in the context. It creates a user, sets a signed JWT as a cookie and Authorization header,
// and adds the user ID to the request context.
func (a *Auth) RegisterNewUser(h http.Handler) http.Handler {
	middleware := func(response http.ResponseWriter, request *http.Request) {
		userID, ok := request.Context().Value(UserIDKey).(string)
		if ok && userID != "" {
			h.ServeHTTP(response, request)

			return
		}
		userID, err := a.db.CreateUser(request.Context(), &user.User{}, nil)
		if err != nil {
			logger.Log.Debugln("Error calling the `a.db.createUser()`: ", zap.Error(err))
			response.WriteHeader(http.StatusInternalServerError)

			return
		}

		JWTString, err := a.buildJWTString(&Claims{UserID: userID})
		if err != nil {
			logger.Log.Debugln("Error calling the `a.buildJWTString()`: ", zap.Error(err))
			response.WriteHeader(http.StatusInternalServerError)

			return
		}

		response.Header().Set("Authorization", JWTString)

		http.SetCookie(
			response,
			&http.Cookie{
				Name:  a.authCookieName,
				Value: JWTString,
			},
		)

		ctx := context.WithValue(request.Context(), UserIDKey, userID)
		requestWithCtx := request.WithContext(ctx)
		h.ServeHTTP(response, requestWithCtx)
	}

	return http.HandlerFunc(middleware)
}

// AuthenticateUser is an HTTP middleware that authenticates incoming requests
// using JWTs found in the Authorization header or cookies.
// It fetches the user from storage and stores the user ID in the request context.
func (a *Auth) AuthenticateUser(h http.Handler) http.Handler {
	middleware := func(response http.ResponseWriter, request *http.Request) {
		userID, err := a.getUserIDFromAuthorizationHeaderOrCookie(request)
		if err != nil {
			logger.Log.Debugln("Error calling the `a.getUserIDFromAuthorizationHeaderOrCookie()`: ", zap.Error(err))
			response.WriteHeader(http.StatusInternalServerError)
			return
		}

		usr, err := a.db.GetUserByID(request.Context(), userID, nil)
		if err != nil {
			logger.Log.Debugln("Error calling the `a.db.GetUserByID()`: ", zap.Error(err))
			response.WriteHeader(http.StatusInternalServerError)
			return
		}

		ctx := context.WithValue(request.Context(), UserIDKey, usr.ID)
		requestWithCtx := request.WithContext(ctx)

		h.ServeHTTP(response, requestWithCtx)
	}

	return http.HandlerFunc(middleware)
}

func (a *Auth) getTokenStringFromAuthorizationHeaderOrCookie(request *http.Request) string {
	tokenString := request.Header.Get("Authorization")
	if tokenString != "" {
		return tokenString
	}
	cookie, err := request.Cookie(a.authCookieName)
	if err == nil {
		tokenString = cookie.Value
	}

	return tokenString
}

func (a *Auth) getUserIDFromAuthorizationHeaderOrCookie(request *http.Request) (string, error) {
	tokenString := a.getTokenStringFromAuthorizationHeaderOrCookie(request)
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return a.authCookieSigningSecretKey, nil
		},
	)
	if err != nil || !token.Valid {
		return "", nil
	}

	return claims.UserID, nil
}

func (a *Auth) buildJWTString(claims *Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, *claims)

	tokenString, err := token.SignedString(a.authCookieSigningSecretKey)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}
