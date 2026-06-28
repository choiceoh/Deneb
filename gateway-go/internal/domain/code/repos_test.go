package code

import (
	"context"
	"testing"
)

func TestListRepos(t *testing.T) {
	fake := &fakeRunner{out: map[string][]byte{
		"repo": []byte(`[{"nameWithOwner":"acme/app"},{"nameWithOwner":"acme/tools"}]`),
	}}
	m := &Manager{Runner: fake}

	repos, err := m.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 2 || repos[0].Owner != "acme" || repos[0].Name != "app" || repos[1].Name != "tools" {
		t.Errorf("repos = %+v", repos)
	}
	wantSeq(t, fake.joined(), []string{
		"gh repo list --limit 200 --json nameWithOwner",
	})
}

func TestListRepos_BadJSON(t *testing.T) {
	fake := &fakeRunner{out: map[string][]byte{"repo": []byte("not json")}}
	m := &Manager{Runner: fake}
	if _, err := m.ListRepos(context.Background()); err == nil {
		t.Error("malformed gh output should error")
	}
}
