package main

import (
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
