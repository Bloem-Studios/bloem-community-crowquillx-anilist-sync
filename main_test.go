package main

import (
	"context"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestConfigure(t *testing.T) {
	account, _ := structpb.NewStruct(map[string]any{"access_token": " anilist ", "profile_id": "p1"})
	siloConfig, _ := structpb.NewStruct(map[string]any{"api_key": " silo ", "base_url": "http://silo/"})
	s := &server{}
	_, err := s.Configure(context.Background(), &pluginv1.ConfigureRequest{Config: []*pluginv1.ConfigEntry{
		{Key: "account", Value: account},
		{Key: "silo", Value: siloConfig},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if s.config.AniListToken != "anilist" || s.config.SiloAPIKey != "silo" || s.config.SiloBaseURL != "http://silo" {
		t.Fatalf("config = %#v", s.config)
	}
}

func TestHandleEventIgnoresNonWatchedChanges(t *testing.T) {
	payload, _ := structpb.NewStruct(map[string]any{"change": "progress", "profile_id": "p1", "content_id": "e1"})
	s := &server{}
	if _, err := s.HandleEvent(context.Background(), &pluginv1.HandleEventRequest{EventName: "user_state.changed", Payload: payload}); err != nil {
		t.Fatal(err)
	}
}
