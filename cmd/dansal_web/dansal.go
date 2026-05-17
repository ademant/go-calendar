package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type DansalClient struct {
	BaseURL string
	HTTP    *http.Client
}

type Event struct {
	ID              int        `json:"id"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	StartTime       string     `json:"start_time"`
	EndTime         string     `json:"end_time"`
	HasBall         bool       `json:"has_ball"`
	HasWorkshop     bool       `json:"has_workshop"`
	IsCancelled     bool       `json:"is_cancelled"`
	Tags            []string   `json:"tags"`
	IsPublished     bool       `json:"is_published"`
	ShortCode       string     `json:"short_code"`
	URL             string     `json:"url,omitempty"`
	ImageURL        string     `json:"image_url,omitempty"`
	OrganizationID  *int       `json:"organization_id,omitempty"`
	Location        string     `json:"location,omitempty"`
	LocationTown    string     `json:"location_town,omitempty"`
	LocationCountry string     `json:"location_country,omitempty"`
	Pricing         *Pricing   `json:"pricing,omitempty"`
	Musicians       []Musician `json:"musicians,omitempty"`
	CreatedAt       string     `json:"created_at"`
}

type Organization struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

type Pricing struct {
	Type     string  `json:"type"`
	Amount   float64 `json:"amount,omitempty"`
	Currency string  `json:"currency,omitempty"`
	Prices   []Price `json:"prices,omitempty"`
}

type Price struct {
	Label  string  `json:"label"`
	Amount float64 `json:"amount"`
}

type Musician struct {
	ID       int    `json:"id"`
	Bandname string `json:"bandname"`
}

type Location struct {
	ID             int    `json:"id"`
	Location       string `json:"location"`
	ShortName      string `json:"short_name,omitempty"`
	Address        string `json:"address"`
	Zipcode        string `json:"zipcode"`
	Town           string `json:"town"`
	Country        string `json:"country,omitempty"`
	Latitude       string `json:"latitude"`
	Longitude      string `json:"longitude"`
	Internetsite   string `json:"internetsite"`
	CreatedAt      string `json:"created_at"`
	OrganizationID *int   `json:"organization_id,omitempty"`
}

type FetchSource struct {
	ID             int      `json:"id"`
	URL            string   `json:"url"`
	Type           string   `json:"type"`
	Tags           []string `json:"tags"`
	OrganizationID *int     `json:"organization_id,omitempty"`
	LastFetchedAt  string   `json:"last_fetched_at,omitempty"`
	CreatedAt      string   `json:"created_at"`
}

type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	User      struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	} `json:"user"`
}

type UserInfo struct {
	ID            int    `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	Role          string `json:"role"`
	Description   string `json:"description"`
	Telegram      string `json:"telegram"`
	Matrix        string `json:"matrix"`
	Mastodon      string `json:"mastodon"`
	Website       string `json:"website"`
	EmailVerified bool   `json:"email_verified"`
	CreatedAt     string `json:"created_at"`
}

func (c *DansalClient) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("not found")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dansal API: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *DansalClient) Login(ctx context.Context, username, password string) (*LoginResponse, error) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/login",
		bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("invalid credentials")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dansal API: %s", resp.Status)
	}
	var lr LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return nil, err
	}
	return &lr, nil
}

func (c *DansalClient) Logout(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+"/api/v1/login", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *DansalClient) GetEvents(ctx context.Context, after string) ([]Event, error) {
	path := "/api/v1/events?is_published=true&include_past=true"
	if after != "" {
		path += "&start_time_after=" + after
	}
	var events []Event
	if err := c.get(ctx, path, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (c *DansalClient) GetEvent(ctx context.Context, id int) (Event, error) {
	var event Event
	if err := c.get(ctx, fmt.Sprintf("/api/v1/events/%d", id), &event); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (c *DansalClient) GetOrganizations(ctx context.Context) ([]Organization, error) {
	var orgs []Organization
	if err := c.get(ctx, "/api/v1/organizations", &orgs); err != nil {
		return nil, err
	}
	return orgs, nil
}

func (c *DansalClient) GetOrganization(ctx context.Context, id int) (Organization, error) {
	var org Organization
	if err := c.get(ctx, fmt.Sprintf("/api/v1/organizations/%d", id), &org); err != nil {
		return Organization{}, err
	}
	return org, nil
}

func (c *DansalClient) authed(ctx context.Context, method, path, token string, body []byte) (*http.Response, error) {
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return c.HTTP.Do(req)
}

func (c *DansalClient) CreateOrganization(ctx context.Context, name, description, token string) (Organization, error) {
	body, _ := json.Marshal(map[string]string{"name": name, "description": description})
	resp, err := c.authed(ctx, http.MethodPost, "/api/v1/organizations", token, body)
	if err != nil {
		return Organization{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return Organization{}, fmt.Errorf("forbidden")
	}
	if resp.StatusCode != http.StatusCreated {
		return Organization{}, fmt.Errorf("dansal API: %s", resp.Status)
	}
	var org Organization
	return org, json.NewDecoder(resp.Body).Decode(&org)
}

func (c *DansalClient) UpdateOrganization(ctx context.Context, id int, name, description, token string) error {
	body, _ := json.Marshal(map[string]string{"name": name, "description": description})
	resp, err := c.authed(ctx, http.MethodPut, fmt.Sprintf("/api/v1/organizations/%d", id), token, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("forbidden")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dansal API: %s", resp.Status)
	}
	return nil
}

func (c *DansalClient) GetFetchSources(ctx context.Context, token string) ([]FetchSource, error) {
	resp, err := c.authed(ctx, http.MethodGet, "/api/v1/fetchurl", token, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dansal API: %s", resp.Status)
	}
	var sources []FetchSource
	return sources, json.NewDecoder(resp.Body).Decode(&sources)
}

func (c *DansalClient) GetFetchSource(ctx context.Context, id int, token string) (FetchSource, error) {
	resp, err := c.authed(ctx, http.MethodGet, fmt.Sprintf("/api/v1/fetchurl/%d", id), token, nil)
	if err != nil {
		return FetchSource{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return FetchSource{}, fmt.Errorf("not found")
	}
	if resp.StatusCode != http.StatusOK {
		return FetchSource{}, fmt.Errorf("dansal API: %s", resp.Status)
	}
	var src FetchSource
	return src, json.NewDecoder(resp.Body).Decode(&src)
}

func (c *DansalClient) UpdateFetchSource(ctx context.Context, id int, typ string, tags []string, orgID *int, token string) error {
	payload := map[string]interface{}{
		"type":            typ,
		"tags":            tags,
		"organization_id": orgID,
	}
	body, _ := json.Marshal(payload)
	resp, err := c.authed(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/fetchurl/%d", id), token, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dansal API: %s", resp.Status)
	}
	return nil
}

func (c *DansalClient) GetLocations(ctx context.Context) ([]Location, error) {
	var locs []Location
	if err := c.get(ctx, "/api/v1/locations", &locs); err != nil {
		return nil, err
	}
	return locs, nil
}

func (c *DansalClient) GetLocation(ctx context.Context, id int) (Location, error) {
	var loc Location
	if err := c.get(ctx, fmt.Sprintf("/api/v1/locations/%d", id), &loc); err != nil {
		return Location{}, err
	}
	return loc, nil
}

func (c *DansalClient) CreateLocation(ctx context.Context, loc Location, token string) (Location, error) {
	body, _ := json.Marshal(loc)
	resp, err := c.authed(ctx, http.MethodPost, "/api/v1/locations", token, body)
	if err != nil {
		return Location{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return Location{}, fmt.Errorf("forbidden")
	}
	if resp.StatusCode != http.StatusCreated {
		return Location{}, fmt.Errorf("dansal API: %s", resp.Status)
	}
	var created Location
	return created, json.NewDecoder(resp.Body).Decode(&created)
}

func (c *DansalClient) UpdateLocation(ctx context.Context, id int, loc Location, token string) error {
	body, _ := json.Marshal(loc)
	resp, err := c.authed(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/locations/%d", id), token, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("forbidden")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dansal API: %s", resp.Status)
	}
	return nil
}

func (c *DansalClient) BulkAssignLocationOrg(ctx context.Context, ids []int, orgID *int, token string) error {
	payload := map[string]interface{}{"ids": ids, "organization_id": orgID}
	body, _ := json.Marshal(payload)
	resp, err := c.authed(ctx, http.MethodPost, "/api/v1/locations/bulk-assign-org", token, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("dansal API: %s", resp.Status)
	}
	return nil
}

func (c *DansalClient) GetEventsByOrg(ctx context.Context, orgID int) ([]Event, error) {
	all, err := c.GetEvents(ctx, "")
	if err != nil {
		return nil, err
	}
	var events []Event
	for _, e := range all {
		if e.OrganizationID != nil && *e.OrganizationID == orgID {
			events = append(events, e)
		}
	}
	return events, nil
}

func (c *DansalClient) GetUser(ctx context.Context, id int, token string) (UserInfo, error) {
	resp, err := c.authed(ctx, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", id), token, nil)
	if err != nil {
		return UserInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UserInfo{}, fmt.Errorf("dansal API: %s", resp.Status)
	}
	var u UserInfo
	return u, json.NewDecoder(resp.Body).Decode(&u)
}

func (c *DansalClient) UpdateUser(ctx context.Context, id int, fields map[string]string, token string) error {
	body, _ := json.Marshal(fields)
	resp, err := c.authed(ctx, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", id), token, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dansal API: %s", resp.Status)
	}
	return nil
}

func (c *DansalClient) SendEmailVerification(ctx context.Context, id int, baseURL, token string) error {
	body, _ := json.Marshal(map[string]string{"channel": "email", "base_url": baseURL})
	resp, err := c.authed(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/%d/verify", id), token, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("dansal API: %s", resp.Status)
	}
	return nil
}

func (c *DansalClient) RequestMagicLogin(ctx context.Context, identifier string) error {
	var payload map[string]string
	if strings.Contains(identifier, "@") {
		payload = map[string]string{"email": identifier}
	} else {
		payload = map[string]string{"username": identifier}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/login/magic",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *DansalClient) UseMagicLogin(ctx context.Context, token string) (*LoginResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/api/v1/login/magic/"+token, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, fmt.Errorf("invalid_or_expired")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dansal API: %s", resp.Status)
	}
	var lr LoginResponse
	return &lr, json.NewDecoder(resp.Body).Decode(&lr)
}

func (c *DansalClient) ConsumeVerification(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/api/v1/verify/"+token, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("invalid")
	}
	if resp.StatusCode == http.StatusGone {
		return fmt.Errorf("expired")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dansal API: %s", resp.Status)
	}
	return nil
}
