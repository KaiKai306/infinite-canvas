package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

// Credentials and upstream sessions live only for this login, never in the database.
type newAPILogin struct {
	client  *http.Client
	base    string
	flow    string
	expires time.Time
}

type NewAPILoginResult struct {
	model.AuthSession
	ChallengeID  string `json:"challengeId,omitempty"`
	RequireTwoFA bool   `json:"requireTwoFA,omitempty"`
}

type newAPIUser struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        int    `json:"role"`
	Status      int    `json:"status"`
}

type newAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		newAPIUser
		User                *newAPIUser `json:"user"`
		AccessToken         string      `json:"access_token"`
		TokenType           string      `json:"token_type"`
		Require2FA          bool        `json:"require_2fa"`
		RequireVerification bool        `json:"require_verification"`
		FlowToken           string      `json:"flow_token"`
	} `json:"data"`
}

var newAPIChallenges = struct {
	sync.Mutex
	items map[string]*newAPILogin
}{items: make(map[string]*newAPILogin)}

func LoginNewAPI(ctx context.Context, username, password, challengeID, code string) (NewAPILoginResult, error) {
	var session *newAPILogin
	var response newAPIResponse
	var err error
	if challengeID != "" {
		newAPIChallenges.Lock()
		session = newAPIChallenges.items[challengeID]
		delete(newAPIChallenges.items, challengeID) // One attempt; a failed code requires a fresh login.
		newAPIChallenges.Unlock()
		if session == nil || time.Now().After(session.expires) {
			return NewAPILoginResult{}, safeMessageError{message: "验证已过期，请重新登录"}
		}
		path, body := "/api/user/login/2fa", map[string]string{"code": strings.TrimSpace(code)}
		if session.flow != "" {
			path = "/api/user/login/verify"
			body["flow_token"], body["method"] = session.flow, "two_fa"
		}
		response, err = session.request(ctx, http.MethodPost, path, body, 0, "")
	} else {
		if strings.TrimSpace(username) == "" || password == "" {
			return NewAPILoginResult{}, safeMessageError{message: "请输入 New API 用户名和密码"}
		}
		jar, _ := cookiejar.New(nil)
		session = &newAPILogin{
			client: &http.Client{Jar: jar, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
			base:   strings.TrimRight(config.Cfg.NewAPIBaseURL, "/"),
		}
		response, err = session.request(ctx, http.MethodPost, "/api/user/login", map[string]string{"username": strings.TrimSpace(username), "password": password}, 0, "")
	}
	if err != nil {
		return NewAPILoginResult{}, err
	}
	if !response.Success {
		return NewAPILoginResult{}, safeMessageError{message: "New API 登录失败，请检查账号、密码或验证码"}
	}
	if response.Data.Require2FA || response.Data.RequireVerification {
		if challengeID != "" {
			return NewAPILoginResult{}, safeMessageError{message: "New API 验证未完成，请重新登录"}
		}
		session.flow = response.Data.FlowToken
		if response.Data.RequireVerification && session.flow == "" {
			return NewAPILoginResult{}, errors.New("New API verification missing flow token")
		}
		session.expires = time.Now().Add(5 * time.Minute)
		id := newID("challenge")
		newAPIChallenges.Lock()
		defer newAPIChallenges.Unlock()
		for key, entry := range newAPIChallenges.items {
			if time.Now().After(entry.expires) {
				delete(newAPIChallenges.items, key)
			}
		}
		if len(newAPIChallenges.items) >= 128 {
			return NewAPILoginResult{}, safeMessageError{message: "登录请求较多，请稍后重试"}
		}
		newAPIChallenges.items[id] = session
		return NewAPILoginResult{ChallengeID: id, RequireTwoFA: true}, nil
	}
	user := response.Data.newAPIUser
	if response.Data.User != nil {
		user = *response.Data.User
	}
	if user.ID <= 0 {
		return NewAPILoginResult{}, errors.New("New API did not return a completed identity")
	}
	access := response.Data.AccessToken
	if response.Data.User != nil || access != "" {
		if access == "" || !strings.EqualFold(response.Data.TokenType, "bearer") || strings.ContainsAny(access, " \t\r\n") {
			return NewAPILoginResult{}, errors.New("New API returned an invalid login session")
		}
	}
	profile, err := session.request(ctx, http.MethodGet, "/api/user/self", nil, user.ID, access)
	if err != nil {
		return NewAPILoginResult{}, err
	}
	if !profile.Success || profile.Data.ID != user.ID || profile.Data.Status != 1 || profile.Data.Username == "" {
		return NewAPILoginResult{}, safeMessageError{message: "New API 账号状态无效或已禁用"}
	}
	return completeNewAPILogin(profile.Data.newAPIUser)
}

func (s *newAPILogin) request(ctx context.Context, method, path string, body any, id int, token string) (newAPIResponse, error) {
	var result newAPIResponse
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return result, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, reader)
	if err != nil {
		return result, errors.New("invalid New API URL")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if id > 0 {
		req.Header.Set("New-Api-User", strconv.Itoa(id))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return result, safeMessageError{message: "New API 暂时无法连接，请稍后重试"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil {
		return result, safeMessageError{message: "New API 登录接口暂时不可用，请稍后重试"}
	}
	return result, nil
}

func completeNewAPILogin(profile newAPIUser) (NewAPILoginResult, error) {
	// Stable namespace; never merge with a local account by name or email.
	id := "newapi-" + strconv.Itoa(profile.ID)
	user, found, err := repository.GetUserByID(id)
	if err != nil {
		return NewAPILoginResult{}, err
	}
	if found && user.Status == model.UserStatusBan {
		return NewAPILoginResult{}, safeMessageError{message: "画布账号已被禁用"}
	}
	if !found {
		user = model.User{ID: id, Username: id, Status: model.UserStatusActive, CreatedAt: now()}
	}
	user.DisplayName = firstNonEmpty(profile.DisplayName, profile.Username)
	user.Role = model.UserRoleUser
	if profile.Role >= 100 {
		user.Role = model.UserRoleAdmin
	}
	user.LastLoginAt, user.UpdatedAt = now(), now()
	user, err = repository.SaveUser(user)
	if err != nil {
		return NewAPILoginResult{}, err
	}
	session, err := newSession(user)
	return NewAPILoginResult{AuthSession: session}, err
}
