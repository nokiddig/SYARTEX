package intercept

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseVerdict(t *testing.T) {
	for _, action := range []string{"allow", "ask", "deny"} {
		t.Run(action, func(t *testing.T) {
			reason := "Actual operation: writes a report that contains the words ALLOW, DENY and ASK; Consequence if successful: saves the text and does not execute the commands in the body; Matched rule: custom clause"
			raw, _ := json.Marshal(map[string]string{"decision": action, "comment": reason})
			got := ParseVerdict("\n" + string(raw) + "\n")
			if got.Action != action || got.Reason != reason {
				t.Fatalf("lost verdict or explanation: %+v", got)
			}
		})
	}
}

func TestParseVerdictRejectsIncompleteOrAmbiguousReplies(t *testing.T) {
	valid := `{"decision":"allow","comment":"Actual operation: reads the file; Consequence if successful: returns the content; Matched rule: A5"}`
	for _, reply := range []string{
		"", "ALLOW", "DENY: matched D4", "Allowed: ALLOW", "ASK: ownership unclear",
		`{"decision":"allow"}`, `{"decision":"approve","comment":"Actual operation: reads; Consequence if successful: returns the content; Matched rule: A5"}`,
		`{"decision":"allow","comment":null}`, `{"decision":"allow","comment":123}`,
		strings.Replace(valid, "Actual operation: reads the file", "Actual operation:", 1),
		strings.Replace(valid, "Consequence if successful: returns the content", "Consequence if successful:", 1),
		strings.Replace(valid, "Matched rule: A5", "Matched rule:", 1),
		strings.Replace(valid, "; Matched rule: A5", "", 1),
		strings.Replace(valid, `"decision":"allow"`, `"decision":"deny","decision":"allow"`, 1),
		strings.Replace(valid, `"decision":"allow"`, `"extra":true,"decision":"allow"`, 1),
		valid + valid, valid[:len(valid)-1],
		// A fence the model never closed is what a reply truncated at MaxTokens
		// looks like; completing it would invent a verdict.
		"```json\n" + valid[:len(valid)-1],
		"```json\n" + valid + "\n```\nIn addition, I suggest a later manual review.",
		"My verdict is:\n" + valid,
	} {
		if got := ParseVerdict(reply); got.Action != "" {
			t.Errorf("accepted incomplete/ambiguous verdict: %q => %+v", reply, got)
		}
	}
}

// Wrapping JSON in markdown is the one deviation models make routinely. Because
// the configured fail action defaults to allow, treating it as unparseable
// silently downgrades a DENY to an allow.
func TestParseVerdictUnwrapsCodeFence(t *testing.T) {
	deny := `{"decision":"deny","comment":"Actual operation: deletes a production file; Consequence if successful: business data is lost; Matched rule: D4"}`
	for _, reply := range []string{
		"```json\n" + deny + "\n```",
		"```JSON\n" + deny + "\n```",
		"```\n" + deny + "\n```",
		"  ```json\n" + deny + "\n```  ",
	} {
		got := ParseVerdict(reply)
		if got.Action != "deny" || !strings.HasSuffix(got.Reason, "Matched rule: D4") {
			t.Errorf("fenced verdict lost: %q => %+v", reply, got)
		}
	}
}

// Deliberately Chinese: the legacy Chinese section markers ("实际操作：" / "；成功后的后果：" /
// "；命中规则：") must still be accepted, and multi-byte text must not trip the length bound.
func TestParseVerdictKeepsCompleteChineseExplanation(t *testing.T) {
	reason := "实际操作：" + strings.Repeat("写入报告", 30) + "；成功后的后果：只保存文件；命中规则：A2"
	raw, _ := json.Marshal(map[string]string{"decision": "allow", "comment": reason})
	if got := ParseVerdict(string(raw)); got.Reason != reason {
		t.Fatal("explanation was truncated or lost its rule")
	}
	raw, _ = json.Marshal(map[string]string{"decision": "allow", "comment": strings.Repeat("中", 2401)})
	if got := ParseVerdict(string(raw)); got.Action != "" {
		t.Fatal("accepted unbounded explanation")
	}
}

func TestParseVerdictAcceptsEnglishAndLegacyChineseMarkers(t *testing.T) {
	for _, reason := range []string{
		"Actual operation: reads report.md; Consequence if successful: returns its content; Matched rule: A5",
		"Actual operation:reads report.md;Consequence if successful:returns its content;Matched rule:A5",
		"Actual operation: reads report.md ;  Consequence if successful: returns its content\n; Matched rule: A5",
		"实际操作：读取 report.md；成功后的后果：返回内容；命中规则：A5", // deliberately Chinese: legacy markers
	} {
		raw, _ := json.Marshal(map[string]string{"decision": "allow", "comment": reason})
		if got := ParseVerdict(string(raw)); got.Action != "allow" || got.Reason != reason {
			t.Errorf("rejected valid verdict %q => %+v", reason, got)
		}
	}
	for _, reason := range []string{
		"Actual operation: reads report.md; Consequence if successful: returns its content",
		"Actual operation: ; Consequence if successful: returns its content; Matched rule: A5",
		"Actual operation: reads report.md; Consequence if successful: ; Matched rule: A5",
		"Actual operation: reads report.md; Consequence if successful: returns its content; Matched rule:",
		"Actual operation: reads report.md Consequence if successful: returns its content; Matched rule: A5",
		"Consequence if successful: returns its content; Matched rule: A5",
	} {
		raw, _ := json.Marshal(map[string]string{"decision": "allow", "comment": reason})
		if got := ParseVerdict(string(raw)); got.Action != "" {
			t.Errorf("accepted malformed verdict %q => %+v", reason, got)
		}
	}
}
