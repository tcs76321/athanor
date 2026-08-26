package main

import (
	"flag"
	"fmt"
	"net/http"
)

func runArtifacts(args []string) error {
	fs := flag.NewFlagSet("artifacts", flag.ContinueOnError)
	projectID := fs.String("project", "", "project id (required)")
	addr := fs.String("addr", defaultAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *projectID == "" {
		return fmt.Errorf("-project is required")
	}
	var out struct {
		Artifacts []struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Version int    `json:"version"`
			Status  string `json:"status"`
		} `json:"artifacts"`
	}
	if err := apiCall(http.MethodGet, *addr+"/projects/"+*projectID+"/artifacts", nil, &out); err != nil {
		return err
	}
	if len(out.Artifacts) == 0 {
		fmt.Println("no artifacts yet")
		return nil
	}
	for _, a := range out.Artifacts {
		fmt.Printf("%s  %-13s v%d  %s\n", a.ID, a.Kind, a.Version, a.Status)
	}
	return nil
}

func runFreeze(args []string) error {
	fs := flag.NewFlagSet("freeze", flag.ContinueOnError)
	addr := fs.String("addr", defaultAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var out struct {
		Frozen  bool   `json:"frozen"`
		Message string `json:"message"`
	}
	if err := apiCall(http.MethodPost, *addr+"/freeze", nil, &out); err != nil {
		return err
	}
	fmt.Println(out.Message)
	return nil
}

func runUnfreeze(args []string) error {
	fs := flag.NewFlagSet("unfreeze", flag.ContinueOnError)
	reason := fs.String("reason", "", "why the daemon may resume (required, §22.2)")
	addr := fs.String("addr", defaultAddr, "daemon address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *reason == "" {
		return fmt.Errorf("-reason is required (recorded in the event log, §22.2)")
	}
	if err := apiCall(http.MethodDelete, *addr+"/freeze", map[string]string{"reason": *reason}, nil); err != nil {
		return err
	}
	fmt.Println("daemon unfrozen; interrupted jobs resume from their checkpoints")
	return nil
}
