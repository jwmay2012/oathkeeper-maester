// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/logr"
)

func fileOperator(t *testing.T) *FilesOperator {
	t.Helper()
	return &FilesOperator{Log: logr.Discard(), RulesFilePath: filepath.Join(t.TempDir(), "access-rules.json")}
}

func TestRulesFileOpenReaderKeepsCompleteSnapshot(t *testing.T) {
	operator := fileOperator(t)
	old := `[{"id":"old"}]`
	if err := os.WriteFile(operator.RulesFilePath, []byte(old), 0640); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(operator.RulesFilePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := operator.updateOrCreateRulesFile(context.Background(), `[{"id":"new"}]`); err != nil {
		t.Fatal(err)
	}
	var seen bytes.Buffer
	if _, err := seen.ReadFrom(reader); err != nil {
		t.Fatal(err)
	}
	if seen.String() != old {
		t.Fatalf("an already-open reader saw a rewritten snapshot: %q", seen.String())
	}
	current, err := os.ReadFile(operator.RulesFilePath)
	if err != nil || string(current) != `[{"id":"new"}]` {
		t.Fatalf("new readers did not see the replacement: %q, %v", current, err)
	}
	info, err := os.Stat(operator.RulesFilePath)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("replacement did not preserve permissions: %v, %v", info, err)
	}
}

func TestRulesFileConcurrentReadersNeverSeePartialJSON(t *testing.T) {
	operator := fileOperator(t)
	first := `[{"id":"first","payload":"` + strings.Repeat("a", 1<<20) + `"}]`
	second := `[{"id":"second","payload":"` + strings.Repeat("b", 1<<20) + `"}]`
	if err := operator.updateOrCreateRulesFile(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var readers sync.WaitGroup
	failures := make(chan string, 1)
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for ctx.Err() == nil {
				data, err := os.ReadFile(operator.RulesFilePath)
				if err != nil || !json.Valid(data) || (string(data) != first && string(data) != second) {
					select {
					case failures <- "reader observed missing, partial or mixed rules":
					default:
					}
					return
				}
			}
		}()
	}
	defer func() { cancel(); readers.Wait() }()
	for i := 0; i < 40; i++ {
		rules := first
		if i%2 == 0 {
			rules = second
		}
		if err := operator.updateOrCreateRulesFile(ctx, rules); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	readers.Wait()
	select {
	case failure := <-failures:
		t.Fatal(failure)
	default:
	}
	files, err := os.ReadDir(filepath.Dir(operator.RulesFilePath))
	if err != nil || len(files) != 1 {
		t.Fatalf("temporary files remain: %v, %v", files, err)
	}
}

func TestRulesFileWriteFailurePreservesExistingPath(t *testing.T) {
	operator := fileOperator(t)
	if err := os.Mkdir(operator.RulesFilePath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := operator.updateOrCreateRulesFile(context.Background(), "[]"); err == nil {
		t.Fatal("expected an error for an unwritable rules target")
	}
	info, err := os.Stat(operator.RulesFilePath)
	if err != nil || !info.IsDir() {
		t.Fatalf("failed write changed the existing target: %v, %v", info, err)
	}
}
