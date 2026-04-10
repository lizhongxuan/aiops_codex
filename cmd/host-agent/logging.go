package main

import (
	"strconv"
	"strings"
)

func summarizeLogText(input string, limit int) string {
	text := strings.TrimSpace(input)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\n", "\\n")
	text = strings.ReplaceAll(text, "\r", "\\r")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "...(" + strconv.Itoa(len(text)) + " chars)"
}
