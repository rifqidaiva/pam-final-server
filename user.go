package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID       int    `json:"id"`
	Email    string `json:"email"`
	Password string `json:"-"`
	Name     string `json:"name"`
}

// MARK: /login
// loginEndpoint is a handler function to login
func loginEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(`{"message": "only POST method allowed"}`))
		return
	}

	r.ParseForm()
	email := r.Form.Get("email")
	password := r.Form.Get("password")

	if email == "" || password == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "email and password are required"}`))
		return
	}

	if !isEmailValid(email) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "email is not valid"}`))
		return
	}

	db, err := sql.Open("sqlite3", "./database.db")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	defer db.Close()

	var user User
	err = db.QueryRow("SELECT id, email, password, name FROM users WHERE email = ?", email).
		Scan(&user.ID, &user.Email, &user.Password, &user.Name)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "email or password is incorrect"}`))
		return
	}

	if !checkPasswordHash(password, user.Password) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "email or password is incorrect"}`))
		return
	}

	// Create a new token object, specifying signing method and the claims
	claims := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"id":    user.ID,
		"email": user.Email,
		"name":  user.Name,
	})

	token, err := claims.SignedString([]byte("secret")) // Should be stored in environment variable
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	w.Write([]byte(`{"token": "` + token + `"}`))
	w.WriteHeader(http.StatusOK)
}

// MARK: /register
// registerEndpoint is a handler function to register
func registerEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(`{"message": "only POST method allowed"}`))
		return
	}

	r.ParseForm()
	email := r.Form.Get("email")
	password := r.Form.Get("password")
	name := r.Form.Get("name")

	if email == "" || password == "" || name == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "email, password, and name are required"}`))
		return
	}

	if !isEmailValid(email) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "email is not valid"}`))
		return
	}

	db, err := sql.Open("sqlite3", "./database.db")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", email).Scan(&count)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	if count > 0 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "email already exists"}`))
		return
	}

	passwordHash := hashPassword(password)

	_, err = db.Exec("INSERT INTO users (email, password, name) VALUES (?, ?, ?)", email, passwordHash, name)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
}

// MARK: /users
// usersEndpoint is a handler function to get all users except the current user
func usersEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(`{"message": "only GET method allowed"}`))
		return
	}

	currentUser, err := getHttpUser(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	db, err := sql.Open("sqlite3", "./database.db")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	defer db.Close()

	rows, err := db.Query("SELECT id, email, name FROM users WHERE id != ?", currentUser.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		err = rows.Scan(&u.ID, &u.Email, &u.Name)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message": "` + err.Error() + `"}`))
			return
		}

		users = append(users, u)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(users)
}

// MARK: /user
// userEndpoint is a handler function to get the user by id if provided in the query or the current user
func userEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(`{"message": "only GET method allowed"}`))
		return
	}

	user, err := getHttpUser(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	// Get user by id if provided in the query
	id := r.URL.Query().Get("id")
	if id != "" {
		db, err := sql.Open("sqlite3", "./database.db")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message": "` + err.Error() + `"}`))
			return
		}

		defer db.Close()

		err = db.QueryRow("SELECT id, email, name FROM users WHERE id = ?", id).Scan(&user.ID, &user.Email, &user.Name)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "user not found"}`))
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"id": "%v", "name": "%s", "email": "%s"}`, user.ID, user.Name, user.Email)))
}

func isEmailValid(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// getHttpUser is a helper function to get the user from the http request
func getHttpUser(r *http.Request) (User, error) {
	token := r.Header.Get("Authorization")
	if token == "" {
		return User{}, errors.New("token is required")
	}

	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	} else {
		return User{}, errors.New("invalid token format")
	}

	claims := jwt.MapClaims{}
	tkn, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte("secret"), nil
	})
	if err != nil {
		return User{}, err
	}

	if !tkn.Valid {
		return User{}, errors.New("invalid token")
	}

	return User{
		ID:    int(claims["id"].(float64)),
		Email: claims["email"].(string),
		Name:  claims["name"].(string),
	}, nil
}

// getHttpUserFromToken is a helper function to get the user from the token
func getHttpUserFromToken(token string) (User, error) {
	if token == "" {
		return User{}, errors.New("token is required")
	}

	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	} else {
		return User{}, errors.New("invalid token format")
	}

	claims := jwt.MapClaims{}
	tkn, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte("secret"), nil
	})
	if err != nil {
		return User{}, err
	}

	if !tkn.Valid {
		return User{}, errors.New("invalid token")
	}

	return User{
		ID:    int(claims["id"].(float64)),
		Email: claims["email"].(string),
		Name:  claims["name"].(string),
	}, nil
}

func hashPassword(password string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}

	return string(hash)
}

func checkPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
