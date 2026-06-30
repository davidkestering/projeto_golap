// Package auth provê autenticação básica: store de usuários (bcrypt), papéis
// (admin/user) e tokens de sessão assinados (HMAC-SHA256, stateless).
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Role é o papel de um usuário.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// User é um usuário cadastrado (a senha fica apenas como hash).
type User struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	Role         Role   `json:"role"`
}

// Store guarda os usuários em memória, com persistência opcional em arquivo JSON.
type Store struct {
	mu    sync.RWMutex
	users map[string]User
	file  string
}

// NewStore cria o store. Se file != "", carrega/salva os usuários nesse JSON.
// Semeia um admin (admin/admin) quando vazio.
func NewStore(file string) (*Store, error) {
	s := &Store{users: map[string]User{}, file: file}
	if file != "" {
		if b, err := os.ReadFile(file); err == nil {
			var list []User
			if err := json.Unmarshal(b, &list); err != nil {
				return nil, fmt.Errorf("auth: lendo %s: %w", file, err)
			}
			for _, u := range list {
				s.users[strings.ToLower(u.Username)] = u
			}
		}
	}
	if len(s.users) == 0 {
		if err := s.Register("admin", "admin", RoleAdmin); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Register cadastra um usuário; erro se já existir ou dados inválidos.
func (s *Store) Register(username, password string, role Role) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return fmt.Errorf("usuário e senha são obrigatórios")
	}
	key := strings.ToLower(username)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[key]; ok {
		return fmt.Errorf("usuário %q já existe", username)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.users[key] = User{Username: username, PasswordHash: string(hash), Role: role}
	return s.saveLocked()
}

// Authenticate valida usuário+senha.
func (s *Store) Authenticate(username, password string) (User, bool) {
	s.mu.RLock()
	u, ok := s.users[strings.ToLower(strings.TrimSpace(username))]
	s.mu.RUnlock()
	if !ok {
		return User{}, false
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return User{}, false
	}
	return u, true
}

// Get devolve um usuário pelo nome.
func (s *Store) Get(username string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[strings.ToLower(strings.TrimSpace(username))]
	return u, ok
}

func (s *Store) saveLocked() error {
	if s.file == "" {
		return nil
	}
	list := make([]User, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.file, b, 0o600)
}

// --- tokens de sessão (HMAC, stateless) ------------------------------------

// SignToken gera um token assinado: base64(payload).base64(hmac), onde payload é
// "username\x1frole\x1fexpiraEm".
func SignToken(secret []byte, u User, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	payload := u.Username + "\x1f" + string(u.Role) + "\x1f" + strconv.FormatInt(exp, 10)
	sig := sign(secret, payload)
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(sig)
}

// VerifyToken valida assinatura e expiração, devolvendo o usuário do token.
func VerifyToken(secret []byte, token string) (User, bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return User{}, false
	}
	enc := base64.RawURLEncoding
	payloadB, err1 := enc.DecodeString(parts[0])
	sigB, err2 := enc.DecodeString(parts[1])
	if err1 != nil || err2 != nil {
		return User{}, false
	}
	payload := string(payloadB)
	if !hmac.Equal(sigB, sign(secret, payload)) {
		return User{}, false
	}
	fields := strings.Split(payload, "\x1f")
	if len(fields) != 3 {
		return User{}, false
	}
	exp, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return User{}, false
	}
	return User{Username: fields[0], Role: Role(fields[1])}, true
}

func sign(secret []byte, payload string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(payload))
	return m.Sum(nil)
}
