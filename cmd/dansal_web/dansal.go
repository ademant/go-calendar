package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
