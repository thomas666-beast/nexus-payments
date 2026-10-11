package bankconfig

import (
	"fmt"
	"os"
	"strings"
)

// Bank describes one bank's connection parameters, parsed from env.
type Bank struct {
	ID   string
	Addr string
}

// ParseEnv reads bank registrations from environment variables.
//
// Format:
//
//	NEXUS_BANKS=bank-a=bank-a:7071,bank-b=bank-b:7071
//
// Empty env returns an empty slice (no banks registered).
func ParseEnv() ([]Bank, error) {
	raw := strings.TrimSpace(os.Getenv("NEXUS_BANKS"))
	if raw == "" {
		return nil, nil
	}

	var banks []Bank
	seen := make(map[string]struct{})

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid bank entry %q: expected id=addr", entry)
		}
		id := strings.TrimSpace(parts[0])
		addr := strings.TrimSpace(parts[1])
		if id == "" || addr == "" {
			return nil, fmt.Errorf("invalid bank entry %q: id and addr required", entry)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("duplicate bank id %q", id)
		}
		seen[id] = struct{}{}
		banks = append(banks, Bank{ID: id, Addr: addr})
	}
	return banks, nil
}
