package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Observe steps judge a REAL device. A real device only ever calls out, so the
// runner does not send anything in such a step: it reads the mock WFM's request
// journal and checks what the device sent (or, for a manifest the device must
// reject, what it did not send).

// ObserveMatch selects journal entries.
type ObserveMatch struct {
	Method    string           `json:"method,omitempty"`
	PathRegex string           `json:"path_regex,omitempty"` // path after the base URL, e.g. ^/api/v1/capabilities/[^/]+$
	Where     []StepValidation `json:"where,omitempty"`      // conditions on the entry: status, headers.*, body.*, response.*
}

// ObserveSpec describes what must be seen ("present") or must not be seen ("absent").
type ObserveSpec struct {
	ObserveMatch
	Expect        string           `json:"expect,omitempty"`         // "present" (default) or "absent"
	Status        []int            `json:"status,omitempty"`         // present: answers that pass (default: any 2xx or 304)
	Check         []StepValidation `json:"check,omitempty"`          // present: checks on the request that was seen
	WithinSeconds int              `json:"within_seconds,omitempty"` // how long to wait (default 180)
	AfterPolls    int              `json:"after_polls,omitempty"`    // absent: the device must first have read the manifest this many times
	SettleSeconds int              `json:"settle_seconds,omitempty"` // absent: extra time given to the device after that (default 15)
	OnlyIf        *ObserveMatch    `json:"only_if,omitempty"`        // absent: the check applies only if the device did this
	OnlyIfReason  string           `json:"only_if_reason,omitempty"` // shown when it does not apply
	ForSeconds    int              `json:"for_seconds,omitempty"`    // absent: watch for this long instead of counting polls (nothing is read from the mock WFM meanwhile)
	Since         string           `json:"since,omitempty"`          // "now": only requests received from this step on
	MarkAfter     bool             `json:"mark_after,omitempty"`     // present: later steps look only at requests after the one seen here
	IfAbsent      string           `json:"if_absent,omitempty"`      // present: "skip" or "skip_scenario" instead of FAIL when the request is not seen
	SkipReason    string           `json:"skip_reason,omitempty"`    // shown with if_absent
	Instruct      string           `json:"instruct,omitempty"`       // printed for the operator before waiting
}

const (
	observeMarkKey  = "_observeMark"
	skipScenarioKey = "_skipScenario"
	ownClientKey    = "_ownClientRequests"
)

func ownSPIFFEID() string {
	cert, err := tls.LoadX509KeyPair(filepath.Join(certDir, "svid-cert.pem"), filepath.Join(certDir, "svid-key.pem"))
	if err != nil {
		return ""
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil || len(leaf.URIs) == 0 {
		return ""
	}
	return leaf.URIs[0].String()
}

// fetchJournal returns the mock WFM's journal entries after `since` and the last sequence number.
func fetchJournal(since int) ([]map[string]interface{}, int, error) {
	client, err := mtlsClient("")
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.Get(fmt.Sprintf("%s/api/v1/test/requests?since=%d", MIAFServer, since))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, 0, fmt.Errorf("request journal answered HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Requests []map[string]interface{} `json:"requests"`
		Last     int                      `json:"last"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, 0, err
	}
	return out.Requests, out.Last, nil
}

// markJournal makes later observe steps look only at requests received from now on.
func markJournal(ctx *TestContext) {
	if _, last, err := fetchJournal(1 << 30); err == nil {
		ctx.Data[observeMarkKey] = last
	}
}

func observeMark(ctx *TestContext) int {
	if v, ok := ctx.Data[observeMarkKey].(int); ok {
		return v
	}
	return 0
}

// observeHolds evaluates one condition on a journal entry. Numbers are compared
// by value also when the expected side comes from a {context} placeholder.
func observeHolds(entry map[string]interface{}, v StepValidation, ctx *TestContext) bool {
	if s, ok := v.Value.(string); ok && (v.Operation == "equals" || v.Operation == "gte" || v.Operation == "greater_than") {
		expected := interpolateHeaderValue(s, ctx)
		actual := extractJSONPath(entry, v.Field)
		if actual == nil {
			return false
		}
		a, aNum := toFloat64(actual)
		e, eErr := strconv.ParseFloat(expected, 64)
		switch v.Operation {
		case "equals":
			if aNum && eErr == nil {
				return a == e
			}
			return fmt.Sprintf("%v", actual) == expected
		case "gte":
			return aNum && eErr == nil && a >= e
		default:
			return aNum && eErr == nil && a > e
		}
	}
	if v.Operation == "contains" {
		// An array contains the value when one of its elements equals it.
		if arr, ok := extractJSONPath(entry, v.Field).([]interface{}); ok {
			want := fmt.Sprintf("%v", v.Value)
			if sv, isStr := v.Value.(string); isStr {
				want = interpolateHeaderValue(sv, ctx)
			}
			for _, x := range arr {
				if fmt.Sprintf("%v", x) == want {
					return true
				}
			}
			return false
		}
	}
	return validateResponse(entry, v, ctx)
}

func observeMatches(entry map[string]interface{}, m ObserveMatch, ctx *TestContext) bool {
	if m.Method != "" && !strings.EqualFold(fmt.Sprintf("%v", entry["method"]), m.Method) {
		return false
	}
	if m.PathRegex != "" {
		re, err := regexp.Compile(interpolateContext(m.PathRegex, ctx))
		if err != nil || !re.MatchString(fmt.Sprintf("%v", entry["path"])) {
			return false
		}
	}
	for _, v := range m.Where {
		if !observeHolds(entry, v, ctx) {
			return false
		}
	}
	return true
}

// deviceEntries keeps the requests of the device under test: a client of this
// WFM other than the runner itself. The first one seen becomes {deviceClient}.
func deviceEntries(entries []map[string]interface{}, ctx *TestContext) []map[string]interface{} {
	own := ownSPIFFEID()
	device, _ := ctx.Data["deviceClient"].(string)
	addr, _ := ctx.Data["deviceAddr"].(string)
	var out []map[string]interface{}
	for _, e := range entries {
		client, _ := e["client"].(string)
		if client == "" {
			// A failed TLS handshake has no client identity: it is attributed by address.
			if e["method"] == "TLS" && addr != "" && e["remote"] == addr {
				out = append(out, e)
			}
			continue
		}
		if client == own {
			// The runner sends no Management Interface requests while it observes a
			// device: requests under its own SPIFFE ID come from a device that was
			// given the CTT's WFM Client SVID. Remembered to say so when no device is seen.
			ctx.Data[ownClientKey] = true
			continue
		}
		if device == "" {
			device = client
			ctx.Data["deviceClient"] = client
			ObservedDevice = client
		}
		if client == device {
			if addr == "" {
				addr, _ = e["remote"].(string)
				ctx.Data["deviceAddr"] = addr
			}
			out = append(out, e)
		}
	}
	return out
}

func describeEntry(e map[string]interface{}) string {
	s := fmt.Sprintf("%v %v → %v", e["method"], e["path"], e["status"])
	if detail := extractJSONPath(e, "response.detail"); detail != nil {
		s += fmt.Sprintf(" (%v", detail)
		if errs, ok := extractJSONPath(e, "response.errors").([]interface{}); ok {
			for _, x := range errs {
				if m, ok := x.(map[string]interface{}); ok && m["error"] != nil {
					s += fmt.Sprintf("; %v", m["error"])
				}
			}
		}
		s += ")"
	}
	return s
}

func runObserve(step TestStep, ctx *TestContext) TestResult {
	spec := step.Observe
	result := TestResult{
		StepID:    step.ID,
		StepName:  step.Name,
		Method:    "OBSERVE",
		Endpoint:  strings.TrimSpace(spec.Method + " " + interpolateContext(spec.PathRegex, ctx)),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	for _, list := range [][]StepValidation{spec.Where, spec.Check} {
		for _, v := range list {
			if !validationOperations[v.Operation] {
				result.Status, result.Reason = "fail", fmt.Sprintf("unsupported validation operation: %q (field '%s')", v.Operation, v.Field)
				return result
			}
		}
	}
	if reason, _ := ctx.Data[skipScenarioKey].(string); reason != "" {
		result.Status, result.Reason = "skip", "not applicable: "+reason
		return result
	}
	within := time.Duration(spec.WithinSeconds) * time.Second
	if within == 0 {
		within = 180 * time.Second
	}
	deadline := time.Now().Add(within)
	if spec.Since == "now" {
		markJournal(ctx)
	}
	mark := observeMark(ctx)
	waiting := false
	if spec.Instruct != "" {
		fmt.Printf("    👉 %s\n", spec.Instruct)
	}

	read := func() ([]map[string]interface{}, bool) {
		all, _, err := fetchJournal(mark)
		if err != nil {
			result.Status, result.Reason = "fail", fmt.Sprintf("could not read the mock WFM's request journal: %v", err)
			return nil, false
		}
		return deviceEntries(all, ctx), true
	}
	wait := func(what string) {
		if !waiting {
			fmt.Printf("    ⏳ waiting for the device: %s (up to %s)\n", what, within)
			waiting = true
		}
		time.Sleep(3 * time.Second)
	}

	if spec.Expect == "absent" {
		// The device must first have had the chance to act on the manifest.
		polls := 0
		for spec.ForSeconds > 0 {
			fmt.Printf("    ⏳ watching the device for %ds\n", spec.ForSeconds)
			time.Sleep(time.Duration(spec.ForSeconds) * time.Second)
			break
		}
		for spec.ForSeconds == 0 {
			entries, ok := read()
			if !ok {
				return result
			}
			polls = 0
			for _, e := range entries {
				status, _ := toFloat64(e["status"])
				if e["method"] == "GET" && e["path"] == "/api/v1/deployments" && (status == 200 || status == 304) {
					polls++
				}
			}
			if polls >= spec.AfterPolls {
				break
			}
			if time.Now().After(deadline) {
				result.Status = "fail"
				result.Reason = fmt.Sprintf("the device read the desired state %d time(s) in %s; %d needed before its reaction can be judged", polls, within, spec.AfterPolls)
				return result
			}
			wait(fmt.Sprintf("to read the desired state %d time(s)", spec.AfterPolls))
		}
		settle := time.Duration(spec.SettleSeconds) * time.Second
		if settle == 0 && spec.ForSeconds == 0 {
			settle = 15 * time.Second
		}
		time.Sleep(settle)
		entries, ok := read()
		if !ok {
			return result
		}
		if spec.OnlyIf != nil {
			applies := false
			for _, e := range entries {
				if observeMatches(e, *spec.OnlyIf, ctx) {
					applies = true
					break
				}
			}
			if !applies {
				result.Status = "skip"
				result.Reason = "not applicable: " + firstNonEmpty(spec.OnlyIfReason, "the device did not do what this check is about")
				return result
			}
		}
		for _, e := range entries {
			if observeMatches(e, spec.ObserveMatch, ctx) {
				result.Status = "fail"
				result.StatusCode = int(func() float64 { f, _ := toFloat64(e["status"]); return f }())
				result.Response = e
				result.Reason = "the device sent a request it must not send here: " + describeEntry(e)
				return result
			}
		}
		result.Status = "pass"
		if spec.ForSeconds > 0 {
			result.Reason = fmt.Sprintf("no such request in %ds", spec.ForSeconds)
		} else {
			result.Reason = fmt.Sprintf("no such request after the device read the desired state %d time(s)", polls)
		}
		return result
	}

	// present
	for {
		entries, ok := read()
		if !ok {
			return result
		}
		for _, e := range entries {
			if !observeMatches(e, spec.ObserveMatch, ctx) {
				continue
			}
			status, _ := toFloat64(e["status"])
			result.StatusCode = int(status)
			result.Response = e
			accepted := len(spec.Status) == 0 && ((status >= 200 && status < 300) || status == 304)
			for _, s := range spec.Status {
				if int(status) == s {
					accepted = true
				}
			}
			if !accepted {
				result.Status, result.Reason = "fail", "the device's request was not accepted: "+describeEntry(e)
				return result
			}
			for _, v := range spec.Check {
				if !observeHolds(e, v, ctx) {
					result.Status = "fail"
					result.Reason = fmt.Sprintf("the device's request %v %v does not meet: %s %s", e["method"], e["path"], v.Field, v.Operation)
					if v.Value != nil {
						result.Reason += fmt.Sprintf(" %v (got %v)", v.Value, extractJSONPath(e, v.Field))
					}
					return result
				}
			}
			for key, path := range step.ExtractContext {
				if val := extractJSONPath(e, path); val != nil {
					ctx.Data[key] = val
				}
			}
			if spec.MarkAfter {
				if seq, ok := toFloat64(e["seq"]); ok {
					ctx.Data[observeMarkKey] = int(seq)
				}
			}
			result.Status = "pass"
			result.Reason = "seen: " + describeEntry(e)
			return result
		}
		if time.Now().After(deadline) {
			if spec.IfAbsent == "skip" || spec.IfAbsent == "skip_scenario" {
				reason := firstNonEmpty(spec.SkipReason, "the device did not send this request")
				if spec.IfAbsent == "skip_scenario" {
					ctx.Data[skipScenarioKey] = reason
				}
				result.Status, result.Reason = "skip", "not applicable: "+reason
				return result
			}
			result.Status = "fail"
			if len(entries) == 0 && ctx.Data["deviceClient"] == nil && ctx.Data[ownClientKey] == true {
				result.Reason = fmt.Sprintf("requests were received, but all with %s, the SPIFFE ID of the CTT's own test client, so none can be attributed to the device. The device and the CTT need different WFM Client SVIDs (the guide uses .../client/vendor-device-1 for the device and .../client/ctt-client for the CTT)", ownSPIFFEID())
			} else if len(entries) == 0 && ctx.Data["deviceClient"] == nil {
				result.Reason = fmt.Sprintf("no request from any device was received in %s — is the device running and pointed at %s?", within, MIAFServer)
			} else {
				result.Reason = fmt.Sprintf("the device did not send this request within %s", within)
			}
			return result
		}
		wait(result.Endpoint)
	}
}
