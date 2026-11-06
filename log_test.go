package main

import (
	"reflect"
	"testing"

	"github.com/xtls/xray-core/common/log"
)

func TestGUIHandlerLogLevel(t *testing.T) {
	messages := []log.Message{
		&log.GeneralMessage{Severity: log.Severity_Debug, Content: "debug"},
		&log.GeneralMessage{Severity: log.Severity_Info, Content: "info"},
		&log.GeneralMessage{Severity: log.Severity_Warning, Content: "warning"},
		&log.GeneralMessage{Severity: log.Severity_Error, Content: "error"},
	}
	tests := []struct {
		level string
		want  []string
	}{
		{"debug", []string{"[Debug] debug", "[Info] info", "[Warning] warning", "[Error] error"}},
		{"info", []string{"[Info] info", "[Warning] warning", "[Error] error"}},
		{"warning", []string{"[Warning] warning", "[Error] error"}},
		{"error", []string{"[Error] error"}},
	}
	for _, tc := range tests {
		var got []string
		h := &guiHandler{level: severityForLogLevel(tc.level), fn: func(line string) {
			got = append(got, line)
		}}
		for _, msg := range messages {
			h.Handle(msg)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("level %s: got %v, want %v", tc.level, got, tc.want)
		}
	}
}

func TestGUIHandlerForConfig(t *testing.T) {
	var got []string
	h := guiHandlerForConfig([]byte(`{"log":{"loglevel":"info"}}`), func(line string) {
		got = append(got, line)
	})
	h.Handle(&log.GeneralMessage{Severity: log.Severity_Debug, Content: "hidden"})
	h.Handle(&log.GeneralMessage{Severity: log.Severity_Error, Content: "shown"})
	if !reflect.DeepEqual(got, []string{"[Error] shown"}) {
		t.Fatalf("got %v", got)
	}
}
