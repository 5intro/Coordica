package domain

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var ChatIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func ChatText(v string, max int, required bool) bool {
	return utf8.ValidString(v) && len(v) <= max && !strings.ContainsRune(v, 0) && (!required || strings.TrimSpace(v) != "")
}
func ChatKind(kind string) bool {
	switch kind {
	case "requirements", "tasks", "blocks", "agents", "runs", "consultations", "proposals":
		return true
	}
	return false
}
func ValidateChatList(q ChatListQuery) error {
	if !ChatKind(q.Kind) || q.Limit < 1 || q.Limit > 20 || q.Cursor != "" && !ChatIdentifier.MatchString(q.Cursor) {
		return Fail("invalid_input", "closed kind, bounded cursor and limit 1..20 required")
	}
	return nil
}
func ValidateChatItem(q ChatItemQuery) error {
	if !ChatKind(q.Kind) || !ChatIdentifier.MatchString(q.ID) {
		return Fail("invalid_input", "valid kind and identifier required")
	}
	return nil
}
func ValidateChatSource(s ChatSource) error {
	if s.ClientID != "" && !ChatIdentifier.MatchString(s.ClientID) || !ChatIdentifier.MatchString(s.SessionID) || !ChatIdentifier.MatchString(s.MessageID) {
		return Fail("invalid_input", "bounded source identifiers required")
	}
	return nil
}
func ValidateConversation(in ConversationInput) error {
	if !ChatText(in.OriginalText, 16<<10, true) || !ChatText(in.AssistantSummary, 4<<10, false) || in.ExpectedGoalVersion < 0 || in.ReplyTo != "" && !ChatIdentifier.MatchString(in.ReplyTo) {
		return Fail("invalid_input", "bounded conversation and goal version required")
	}
	return ValidateChatSource(in.Source)
}
func ValidateChatRequirement(in ChatRequirementInput) error {
	if !ChatText(in.Title, 512, true) || !ChatText(in.OriginalText, 16<<10, true) || !ChatText(in.AssistantSummary, 4<<10, false) {
		return Fail("invalid_input", "bounded requirement title, original text and summary required")
	}
	return ValidateChatSource(in.Source)
}
