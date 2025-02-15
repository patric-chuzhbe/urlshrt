package auth

import (
	"context"
	"fmt"
	"github.com/golang-jwt/jwt/v4"
	"github.com/patric-chuzhbe/urlshrt/internal/db/storage"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
	"go.uber.org/zap"
	"net/http"
)

type Auth struct {
	db                         storage.Storage
	authCookieName             string
	authCookieSigningSecretKey []byte
}

type Config struct {
	AuthCookieName             string
	AuthCookieSigningSecretKey []byte
}

type Claims struct {
	jwt.RegisteredClaims
	UserID int `json:"user_id"`
}

type ContextKey string

const UserIDKey ContextKey = "userID"

func (a *Auth) RegisterNewUser(h http.Handler) http.Handler {
	middleware := func(response http.ResponseWriter, request *http.Request) {
		userID, ok := request.Context().Value(UserIDKey).(int)
		if ok && userID > 0 {
			h.ServeHTTP(response, request)

			return
		}
		userID, err := a.db.CreateUser(context.Background(), &user.User{}, nil)
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

func (a *Auth) getUserIDFromAuthorizationHeader(request *http.Request) (int, error) {
	tokenString := request.Header.Get("Authorization")
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
		return 0, nil
	}

	return claims.UserID, nil
}

func (a *Auth) AuthenticateUser(h http.Handler) http.Handler {
	middleware := func(response http.ResponseWriter, request *http.Request) {
		userID, err := a.getUserIDFromAuthorizationHeader(request)
		if err != nil {
			logger.Log.Debugln("Error calling the `a.getUserIDFromAuthorizationHeader()`: ", zap.Error(err))
			response.WriteHeader(http.StatusInternalServerError)
			return
		}

		usr, err := a.db.GetUserByID(context.Background(), userID, nil)
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

func New(
	theDB storage.Storage,
	authCookieName string,
	authCookieSigningSecretKey []byte,
) *Auth {
	return &Auth{
		db:                         theDB,
		authCookieName:             authCookieName,
		authCookieSigningSecretKey: authCookieSigningSecretKey,
	}
}

func (a *Auth) buildJWTString(claims *Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, *claims)

	tokenString, err := token.SignedString(a.authCookieSigningSecretKey)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}
