package agent

import (
	"strings"
	"testing"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
)

func TestFilterToolsByClearance_NoReadUp(t *testing.T) {
	tools := []inference.ToolDefinition{
		{Name: "email_send", Clearance: 2},
		{Name: "finances_read", Clearance: 5},
	}
	filtered, clearances := filterToolsByClearance(tools, 3)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered tool, got %d", len(filtered))
	}
	if filtered[0].Name != "email_send" {
		t.Errorf("expected email_send, got %q", filtered[0].Name)
	}
	if _, ok := clearances["finances_read"]; !ok {
		t.Error("expected finances_read in clearances map")
	}
}

func TestFilterToolsByClearance_NoWriteDown(t *testing.T) {
	tools := []inference.ToolDefinition{
		{Name: "email_send", Description: "Send an email.", Clearance: 2},
	}
	filtered, _ := filterToolsByClearance(tools, 4)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered tool, got %d", len(filtered))
	}
	if filtered[0].Clearance != 2 {
		t.Errorf("expected clearance 2, got %d", filtered[0].Clearance)
	}
	if filtered[0].Description == "Send an email." {
		t.Error("expected description to be annotated with write-down warning")
	}
	if !strings.Contains(filtered[0].Description, "write-down risk") {
		t.Errorf("expected write-down warning in description, got %q", filtered[0].Description)
	}
}

func TestFilterToolsByClearance_EqualClearance(t *testing.T) {
	tools := []inference.ToolDefinition{
		{Name: "email_send", Description: "Send an email.", Clearance: 3},
	}
	filtered, _ := filterToolsByClearance(tools, 3)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered tool, got %d", len(filtered))
	}
	if filtered[0].Description != "Send an email." {
		t.Errorf("expected description unchanged, got %q", filtered[0].Description)
	}
}

func TestFilterToolsByClearance_Mixed(t *testing.T) {
	tools := []inference.ToolDefinition{
		{Name: "web_search", Description: "Search the web.", Clearance: 1},
		{Name: "email_send", Description: "Send an email.", Clearance: 2},
		{Name: "calendar_read", Description: "Read calendar.", Clearance: 3},
		{Name: "finances_read", Description: "Read finances.", Clearance: 5},
	}
	filtered, clearances := filterToolsByClearance(tools, 3)

	if len(filtered) != 3 {
		t.Fatalf("expected 3 filtered tools, got %d", len(filtered))
	}

	// web_search (1 < 3) — annotated
	if !strings.Contains(filtered[0].Description, "write-down risk") {
		t.Errorf("expected web_search annotated, got %q", filtered[0].Description)
	}
	// email_send (2 < 3) — annotated
	if !strings.Contains(filtered[1].Description, "write-down risk") {
		t.Errorf("expected email_send annotated, got %q", filtered[1].Description)
	}
	// calendar_read (3 == 3) — unchanged
	if filtered[2].Description != "Read calendar." {
		t.Errorf("expected calendar_read unchanged, got %q", filtered[2].Description)
	}
	// finances_read (5 > 3) — dropped but in map
	if _, ok := clearances["finances_read"]; !ok {
		t.Error("expected finances_read in clearances map")
	}
}

func TestToolEffect_BLPReadUp(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{Name: "test", RawPermissions: []string{}})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"finances_read": 5},
		agent:              ag,
	}
	if got := h.toolEffect("finances_read"); got != "deny" {
		t.Errorf("toolEffect(finances_read) = %q, want deny", got)
	}
}

func TestToolEffect_BLPWriteDown(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{Name: "test", RawPermissions: []string{"tool:invoke[email_send]"}})
	h := &InferenceHandler{
		effectiveClearance: 4,
		toolClearances:     map[string]int{"email_send": 2},
		agent:              ag,
	}
	if got := h.toolEffect("email_send"); got != "require_confirmation" {
		t.Errorf("toolEffect(email_send) = %q, want require_confirmation", got)
	}
}

func TestToolEffect_BLPEqualClearance(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{Name: "test", RawPermissions: []string{"tool:invoke[*]"}})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"calendar_read": 3},
		agent:              ag,
	}
	if got := h.toolEffect("calendar_read"); got != "allow" {
		t.Errorf("toolEffect(calendar_read) = %q, want allow", got)
	}
}

func TestToolEffect_BLPEqualClearanceNoPermission(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{Name: "test", RawPermissions: []string{}})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"calendar_read": 3},
		agent:              ag,
	}
	if got := h.toolEffect("calendar_read"); got != "deny" {
		t.Errorf("toolEffect(calendar_read) = %q, want deny", got)
	}
}

func TestToolEffect_WildcardPermission(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{Name: "test", RawPermissions: []string{"tool:invoke[*]"}})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"calendar_read": 3},
		agent:              ag,
	}
	if got := h.toolEffect("calendar_read"); got != "allow" {
		t.Errorf("toolEffect(calendar_read) = %q, want allow", got)
	}
}

func TestToolEffect_BLPMissingFromMap(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{Name: "test", RawPermissions: []string{"tool:invoke[unknown]"}})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{},
		agent:              ag,
	}
	// Missing tool defaults to 0 clearance, which is < effectiveClearance (3)
	// so it should require_confirmation.
	if got := h.toolEffect("unknown"); got != "require_confirmation" {
		t.Errorf("toolEffect(unknown) = %q, want require_confirmation", got)
	}
}

func TestToolEffect_BLPEqualClearanceSpecificPermission(t *testing.T) {
	ag, _ := NewAgent(&config.AgentDefinition{
		Name:           "test",
		RawPermissions: []string{"tool:invoke[email_send]"},
	})
	h := &InferenceHandler{
		effectiveClearance: 3,
		toolClearances:     map[string]int{"email_send": 3},
		agent:              ag,
	}
	if got := h.toolEffect("email_send"); got != "allow" {
		t.Errorf("toolEffect(email_send) = %q, want allow", got)
	}
}
