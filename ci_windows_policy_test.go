package main

import (
	"os"
	"strings"
	"testing"
)

func TestPowerShellWorkflowBlocksFailOnEveryNativeCommand(t *testing.T) {
	t.Parallel()

	const block = "shell: pwsh\n        run: |"
	const guardedBlock = "shell: pwsh\n        run: |\n          $ErrorActionPreference = 'Stop'\n          $PSNativeCommandUseErrorActionPreference = $true"
	for _, path := range []string{".github/workflows/ci.yml", ".github/workflows/release-linux.yml"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		workflow := strings.ReplaceAll(string(content), "\r\n", "\n")
		blocks := strings.Count(workflow, block)
		if blocks == 0 {
			t.Fatalf("%s has no multiline PowerShell blocks", path)
		}
		if guarded := strings.Count(workflow, guardedBlock); guarded != blocks {
			t.Fatalf("%s guards %d/%d multiline PowerShell blocks against masked native-command failures", path, guarded, blocks)
		}
	}
}
