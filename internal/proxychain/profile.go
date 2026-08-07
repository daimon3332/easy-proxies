package proxychain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const MaxHops = 8

type Profile struct {
	ID      string `yaml:"id" json:"id"`
	Name    string `yaml:"name" json:"name"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Hops    []Hop  `yaml:"hops" json:"hops"`
}

type Hop struct {
	URI string `yaml:"uri" json:"uri"`
}

func NormalizeProfiles(profiles []Profile) ([]Profile, error) {
	result := make([]Profile, 0, len(profiles))
	ids := make(map[string]struct{}, len(profiles))
	for index, profile := range profiles {
		normalized, err := NormalizeProfile(profile)
		if err != nil {
			return nil, fmt.Errorf("chain profile %d: %w", index+1, err)
		}
		if _, exists := ids[normalized.ID]; exists {
			return nil, fmt.Errorf("duplicate chain profile id %q", normalized.ID)
		}
		ids[normalized.ID] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func NormalizeProfile(profile Profile) (Profile, error) {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.Name = strings.TrimSpace(profile.Name)
	if len(profile.Hops) == 0 {
		return Profile{}, fmt.Errorf("at least one hop is required")
	}
	if len(profile.Hops) > MaxHops {
		return Profile{}, fmt.Errorf("at most %d hops are supported", MaxHops)
	}
	seen := make(map[string]struct{}, len(profile.Hops))
	for index := range profile.Hops {
		profile.Hops[index].URI = strings.TrimSpace(profile.Hops[index].URI)
		if profile.Hops[index].URI == "" {
			return Profile{}, fmt.Errorf("hop %d URI is empty", index+1)
		}
		parsed, err := url.Parse(profile.Hops[index].URI)
		if err != nil || parsed.Scheme == "" {
			return Profile{}, fmt.Errorf("hop %d URI is invalid", index+1)
		}
		identity := strings.ToLower(parsed.Scheme) + ":" + profile.Hops[index].URI[len(parsed.Scheme)+1:]
		if _, exists := seen[identity]; exists {
			return Profile{}, fmt.Errorf("hop %d duplicates an earlier hop", index+1)
		}
		seen[identity] = struct{}{}
	}
	if profile.ID == "" {
		profile.ID = "chain-" + ContentID(profile)[:12]
	}
	if profile.Name == "" {
		profile.Name = profile.ID
	}
	return profile, nil
}

func ContentID(profile Profile) string {
	h := sha256.New()
	for _, hop := range profile.Hops {
		h.Write([]byte(strings.TrimSpace(hop.URI)))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func RouteID(terminalURI string, profile *Profile) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(terminalURI)))
	h.Write([]byte{0})
	if profile != nil {
		h.Write([]byte(ContentID(*profile)))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func Find(profiles []Profile, id string) (Profile, bool) {
	id = strings.TrimSpace(id)
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return Profile{}, false
}

func RedactURI(rawURI string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURI))
	if err != nil || parsed.Scheme == "" {
		return "<invalid-proxy-uri>"
	}
	if parsed.User != nil {
		parsed.User = url.User("***")
	}
	parsed.Fragment = ""
	return parsed.String()
}

func SortedIDs(profiles []Profile) []string {
	ids := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		ids = append(ids, profile.ID)
	}
	sort.Strings(ids)
	return ids
}
