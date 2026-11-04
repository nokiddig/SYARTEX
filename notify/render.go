package notify

import (
	"strings"
	"unicode/utf8"
)

const ellipsis = "…"

// TruncateBytes truncates s to at most max bytes, guaranteeing the result is valid UTF-8 and no character is cut in half.
//
// Why it must cut on character boundaries: the WeCom group bot's markdown has a hard limit of 4096 **bytes** (not
// characters), and a Chinese character takes 3 bytes. Slicing by bytes would cut a Han character in half and
// produce invalid UTF-8: the platform then either rejects the whole message or shows garbled boxes. The approach
// here is to step back from the budget position to the nearest rune start byte (utf8.RuneStart identifies
// continuation bytes 0b10xxxxxx).
//
// max<=0 means no limit. An ellipsis is appended after truncation unless max is too small to fit it.
func TruncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	budget := max - len(ellipsis)
	suffix := ellipsis
	if budget < 0 {
		// max is shorter than the ellipsis itself: drop the ellipsis and truncate plainly, so the result does not exceed max instead.
		budget = max
		suffix = ""
	}
	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}

// OneLine squeezes multi-line text into a single line: it collapses all whitespace, then truncates by character count.
// Used for the title line of IM messages: summaries often contain line breaks, which would break the layout if put straight into a table or title.
// max<=0 means no length limit.
func OneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	return TruncateRunes(s, max)
}

// TruncateRunes truncates s to at most max characters (not bytes), appending an ellipsis when it is exceeded.
// max<=0 means no limit.
//
// The difference from TruncateBytes is the platform's measuring convention: WeCom limits by bytes, Telegram limits
// by character count. Using the wrong convention raises no error but cuts the message far shorter than expected
// (one Chinese character = 3 bytes, so cutting 4096 by bytes leaves only about 1365 characters), so both functions
// must be kept and chosen per channel.
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + ellipsis
}

// TruncateHTML truncates an HTML fragment by character count, guaranteeing no half tag is produced.
//
// Cutting HTML directly by characters yields broken tags such as `<a href="htt`; the platform parser either
// errors and rejects the whole message, or swallows the following body text as an attribute value. The approach
// here: truncate by characters first, then check whether the tail has an unclosed `<`, and if so back up to before it.
//
// Tags are not balanced (appending </b> and the like): Telegram's HTML parser closes unclosed tags automatically,
// while implementing balancing ourselves would have to handle quotes in attributes, comments and self-closing
// tags, and the complexity is out of proportion to the benefit.
func TruncateHTML(s string, max int) string {
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	cut := TruncateRunes(s, max)
	// If the tail is a fragment starting with `<` (no `>` after the last `<`), back up to before the `<`.
	if lt := strings.LastIndex(cut, "<"); lt >= 0 && !strings.Contains(cut[lt:], ">") {
		cut = cut[:lt]
	}
	// If the tail is a truncated HTML entity (such as `&amp;` cut into `&amp`), back up in the same way.
	// A broken entity can make a parser that only accepts entities reject the **whole message**: a batch message over
	// the length limit is common anyway, and it is not worth losing the entire notification over this.
	if amp := strings.LastIndex(cut, "&"); amp >= 0 && !strings.Contains(cut[amp:], ";") {
		cut = cut[:amp]
	}
	return cut
}

// packItemCount computes how many items fit **completely** within the budget, so batch messages are packed by whole items.
//
// Why pack whole items instead of rendering everything and then truncating: truncation makes the trailing items
// vanish into thin air while their delivery records are still marked as delivered, and neither the message nor
// the delivery history shows it: the findings are simply lost. With whole-item packing, items that do not fit
// stay in the database and become the next batch, and the kept value the caller gets is the number of items
// this message really delivered.
//
// Parameters: maxSize<=0 means no limit; reserve is the amount reserved for the message header/footer; size does
// the measuring (platforms differ: WeCom/DingTalk by bytes, Telegram by character count; using the wrong
// convention raises no error but squeezes Chinese messages far below the limit); render renders item idx into
// its actual text, whose length varies with content and cannot be estimated.
//
// Returns at least 1 (as long as items remain). Even when a single item is extremely long, this one must still
// be sent and left to the caller's final truncation, otherwise one oversized finding would block the whole
// batch forever.
func packItemCount(items []Item, maxSize, reserve int, footer string, size func(string) int, render func(Item, int) string) int {
	if maxSize <= 0 {
		return len(items)
	}
	budget := maxSize - reserve - size(footer)
	if budget < 0 {
		budget = 0
	}
	used := 0
	for i, it := range items {
		used += size(render(it, i))
		if used > budget && i > 0 {
			return i
		}
	}
	return len(items)
}

// byteSize / runeSize are the two measuring conventions of packItemCount. They are named so call sites do not
// contain a bare func(s string) int closure, which would make it hard to tell at a glance which convention is used.
func byteSize(s string) int { return len(s) }
func runeSize(s string) int { return utf8.RuneCountInString(s) }

// assetLine renders the asset list as one line of display text; when there are more than limit assets the rest are omitted and the total is noted.
// A finding may be anchored to dozens of assets, and listing them all would blow up the message.
func assetLine(assets []string, limit int) string {
	if len(assets) == 0 {
		return ""
	}
	if limit <= 0 || len(assets) <= limit {
		return strings.Join(assets, ", ")
	}
	return strings.Join(assets[:limit], ", ") + " and others (" + itoa(len(assets)) + " total)"
}

// itoa is a short alias of strconv.Itoa, used only to build display text and to avoid importing strconv everywhere.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
