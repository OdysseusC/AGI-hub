package app

import (
	"context"
	"testing"
	"time"

	"agihub/internal/manager"
)

func TestRemoteClaudeLanguagePropagatesAndReplayKeepsOriginalHint(t *testing.T) {
	a, b := networkFixture(t, "language-a"), networkFixture(t, "language-b")
	remoteClaude := b.Broker.Snapshot().Endpoints["codex"]
	remoteClaude.Driver = "claude-code" // Deliberately misleading endpoint name.
	if err := b.Broker.Register(remoteClaude); err != nil {
		t.Fatal(err)
	}
	invite, err := a.management.mesh.Invite(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request, err := b.management.mesh.RequestPair(context.Background(), invite.Code)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.management.mesh.AcceptPair(context.Background(), request.ID); err != nil {
		t.Fatal(err)
	}
	awaitNetwork(t, func() bool {
		peers := a.remoteCatalog("codex")
		return len(peers) == 1 && peers[0].Driver == "claude-code" && peers[0].SendLanguage == "en"
	})
	hb := b.management.mesh.Snapshot().HubID
	body := manager.MailSubmission{ID: "language-network", Recipient: hb + "/codex", Kind: "request", Content: `Review the quote "中文原文".`, OriginRef: "user-entry"}
	first, err := a.publishRemote("codex", body)
	if err != nil || first.CommunicationLanguage != "en" {
		t.Fatal("remote default language missing", first, err)
	}
	awaitNetwork(t, func() bool { return b.Broker.Snapshot().Messages[first.ID].CommunicationLanguage == "en" })
	w := managementCall(t, b, "/v1/manage/agent", `{"endpoint":"codex","action":"language","communication_language":"zh"}`, b.Config.AdminToken)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	awaitNetwork(t, func() bool {
		peers := a.remoteCatalog("codex")
		return len(peers) == 1 && peers[0].SendLanguage == "zh"
	})
	retry, err := a.publishRemote("codex", body)
	if err != nil || retry.ID != first.ID || retry.CommunicationLanguage != "en" {
		t.Fatal("retry changed after remote language setting changed", retry, err)
	}
	body.ID, body.Content = "language-network-next", "核对原文。"
	body.CorrelationID = first.CorrelationID
	second, err := a.publishRemote("codex", body)
	if err != nil || second.CommunicationLanguage != "zh" {
		t.Fatal("explicit remote Chinese was not applied", second, err)
	}
	awaitNetwork(t, func() bool { return b.Broker.Snapshot().Messages[second.ID].CommunicationLanguage == "zh" })
	if b.Broker.Snapshot().Messages[first.ID].Content != first.Content {
		t.Fatal("transport translated or duplicated the message")
	}
}
