package cmd

import (
	"os"
	"reflect"
	"testing"
)

func TestStripConfigFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantArgs []string
		wantCfg  string
	}{
		{
			name:     "short flag before command",
			args:     []string{"chronos", "-c", "app.yaml", "team", "show", "pipeline"},
			wantArgs: []string{"chronos", "team", "show", "pipeline"},
			wantCfg:  "app.yaml",
		},
		{
			name:     "long flag after command",
			args:     []string{"chronos", "team", "list", "--config", "app.yaml"},
			wantArgs: []string{"chronos", "team", "list"},
			wantCfg:  "app.yaml",
		},
		{
			name:     "equals form",
			args:     []string{"chronos", "--config=cfg/app.yaml", "agent", "list"},
			wantArgs: []string{"chronos", "agent", "list"},
			wantCfg:  "cfg/app.yaml",
		},
		{
			name:     "short equals form",
			args:     []string{"chronos", "-c=app.yaml", "run", "hi"},
			wantArgs: []string{"chronos", "run", "hi"},
			wantCfg:  "app.yaml",
		},
		{
			name:     "no flag leaves args unchanged",
			args:     []string{"chronos", "team", "run", "pipeline", "do it"},
			wantArgs: []string{"chronos", "team", "run", "pipeline", "do it"},
			wantCfg:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origArgs := os.Args
			origCfg, hadCfg := os.LookupEnv("CHRONOS_CONFIG")
			t.Cleanup(func() {
				os.Args = origArgs
				if hadCfg {
					os.Setenv("CHRONOS_CONFIG", origCfg)
				} else {
					os.Unsetenv("CHRONOS_CONFIG")
				}
			})

			os.Unsetenv("CHRONOS_CONFIG")
			os.Args = append([]string{}, tc.args...)
			stripConfigFlag()

			if !reflect.DeepEqual(os.Args, tc.wantArgs) {
				t.Errorf("os.Args = %v, want %v", os.Args, tc.wantArgs)
			}
			if got := os.Getenv("CHRONOS_CONFIG"); got != tc.wantCfg {
				t.Errorf("CHRONOS_CONFIG = %q, want %q", got, tc.wantCfg)
			}
		})
	}
}
