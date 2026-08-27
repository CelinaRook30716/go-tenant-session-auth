package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"
)

type server struct {
	store   *accountStore
	captcha interface {
		Verify(context.Context, string) error
	}
}

type signupInput struct {
	TenantName   string `json:"tenant_name"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	CaptchaToken string `json:"captcha_token"`
}

type loginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type stateInput struct {
	State accountState `json:"state"`
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	s := &server{store: newAccountStore(), captcha: &captchaClient{apiKey: key, widgetRecordID: os.Getenv("INFRAI_WIDGET_RECORD_ID"), http: &http.Client{Timeout: 8 * time.Second}, url: captchaVerifyURL, sleep: sleepContext}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /signup", s.signup)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("GET /me", s.me)
	mux.HandleFunc("PATCH /admin/users/{id}", s.setUserState)
	log.Printf("tenant auth listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func (s *server) signup(w http.ResponseWriter, r *http.Request) {
	var in signupInput
	if !decodeJSON(w, r, &in) || in.TenantName == "" || in.Email == "" || len(in.Password) < 12 || in.CaptchaToken == "" {
		writeError(w, http.StatusBadRequest, "tenant_name, email, captcha_token, and a 12-character password are required")
		return
	}
	if err := s.captcha.Verify(r.Context(), in.CaptchaToken); err != nil {
		var rejected *InfraiError
		if errors.As(err, &rejected) && rejected.HTTPStatus >= 400 && rejected.HTTPStatus < 500 {
			writeError(w, rejected.HTTPStatus, rejected.Message)
			return
		}
		writeError(w, http.StatusBadGateway, "captcha verification could not be completed")
		return
	}
	t, a, err := s.store.onboard(in.TenantName, in.Email, in.Password)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"tenant": t, "account": a})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in loginInput
	if !decodeJSON(w, r, &in) {
		return
	}
	sess, err := s.store.login(in.Email, in.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "tenant_session", Value: sess.ID, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, Expires: sess.ExpiresAt})
	writeJSON(w, http.StatusOK, map[string]string{"status": "authenticated"})
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	a, ok := s.currentAccount(w, r)
	if ok {
		writeJSON(w, http.StatusOK, a)
	}
}

func (s *server) setUserState(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.currentAccount(w, r)
	if !ok {
		return
	}
	var in stateInput
	if !decodeJSON(w, r, &in) || (in.State != stateActive && in.State != stateSuspended) {
		writeError(w, http.StatusBadRequest, "state must be active or suspended")
		return
	}
	if err := s.store.setUserState(actor.ID, r.PathValue("id"), in.State); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user_id": r.PathValue("id"), "state": in.State})
}

func (s *server) currentAccount(w http.ResponseWriter, r *http.Request) (*account, bool) {
	cookie, err := r.Cookie("tenant_session")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	a, err := s.store.authenticate(cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	return a, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
