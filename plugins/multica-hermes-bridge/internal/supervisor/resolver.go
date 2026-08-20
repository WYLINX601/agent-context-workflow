package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type ProfileResolver struct {
	Executable string
}

type profileList struct {
	SchemaVersion int `json:"schema_version"`
	Profiles      *[]struct {
		Name string `json:"name"`
	} `json:"profiles"`
}

func (r ProfileResolver) Exists(ctx context.Context, profile string) (bool, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return true, nil
	}
	if strings.TrimSpace(r.Executable) == "" {
		return false, &SupervisorError{Code: CodeProfileLookup, Message: "Hermes executable is not configured"}
	}
	output, err := exec.CommandContext(ctx, r.Executable, "profile", "list", "--json").Output()
	if err != nil {
		return false, &SupervisorError{Code: CodeProfileLookup, Message: "Hermes profile JSON command failed"}
	}
	var list profileList
	if err := json.Unmarshal(output, &list); err != nil || list.SchemaVersion != 1 || list.Profiles == nil {
		return false, &SupervisorError{Code: CodeProfileLookup, Message: "Hermes profile JSON response is invalid"}
	}
	for _, item := range *list.Profiles {
		if strings.TrimSpace(item.Name) == profile {
			return true, nil
		}
	}
	return false, &SupervisorError{Code: CodeProfileNotFound, Message: fmt.Sprintf("Hermes profile %q does not exist", profile)}
}
