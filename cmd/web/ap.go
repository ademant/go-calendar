package main

var APContext = []interface{}{
	"https://www.w3.org/ns/activitystreams",
	"https://w3id.org/security/v1",
}

type PublicKey struct {
	ID           string `json:"id"`
	Owner        string `json:"owner"`
	PublicKeyPem string `json:"publicKeyPem"`
}

type Actor struct {
	Context           interface{} `json:"@context"`
	Type              string      `json:"type"`
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Summary           string      `json:"summary,omitempty"`
	URL               string      `json:"url,omitempty"`
	PreferredUsername string      `json:"preferredUsername"`
	Inbox             string      `json:"inbox"`
	Outbox            string      `json:"outbox"`
	Followers         string      `json:"followers"`
	PublicKey         PublicKey   `json:"publicKey"`
}

type Activity struct {
	Context interface{} `json:"@context,omitempty"`
	Type    string      `json:"type"`
	ID      string      `json:"id"`
	Actor   string      `json:"actor"`
	Object  interface{} `json:"object"`
	To      []string    `json:"to,omitempty"`
	CC      []string    `json:"cc,omitempty"`
}

type APPlace struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type APHashtag struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Href string `json:"href"`
}

type APEvent struct {
	Context   interface{} `json:"@context,omitempty"`
	Type      string      `json:"type"`
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Content   string      `json:"content,omitempty"`
	StartTime string      `json:"startTime,omitempty"`
	EndTime   string      `json:"endTime,omitempty"`
	Location  *APPlace    `json:"location,omitempty"`
	URL       string      `json:"url,omitempty"`
	Tag       []APHashtag `json:"tag,omitempty"`
}

type OrderedCollection struct {
	Context    interface{} `json:"@context"`
	Type       string      `json:"type"`
	ID         string      `json:"id"`
	TotalItems int         `json:"totalItems"`
	First      string      `json:"first,omitempty"`
	Items      []string    `json:"orderedItems,omitempty"`
}

type OrderedCollectionPage struct {
	Context      interface{}   `json:"@context"`
	Type         string        `json:"type"`
	ID           string        `json:"id"`
	PartOf       string        `json:"partOf"`
	TotalItems   int           `json:"totalItems"`
	OrderedItems []interface{} `json:"orderedItems"`
	Next         string        `json:"next,omitempty"`
	Prev         string        `json:"prev,omitempty"`
}

type WebFingerLink struct {
	Rel  string `json:"rel"`
	Type string `json:"type,omitempty"`
	Href string `json:"href,omitempty"`
}

type WebFinger struct {
	Subject string          `json:"subject"`
	Aliases []string        `json:"aliases,omitempty"`
	Links   []WebFingerLink `json:"links"`
}

type NodeInfoSoftware struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type NodeInfoUsage struct {
	Users struct {
		Total int `json:"total"`
	} `json:"users"`
}

type NodeInfo struct {
	Version           string           `json:"version"`
	Software          NodeInfoSoftware `json:"software"`
	Protocols         []string         `json:"protocols"`
	Usage             NodeInfoUsage    `json:"usage"`
	OpenRegistrations bool             `json:"openRegistrations"`
}
