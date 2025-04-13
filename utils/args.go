package utils

import (
	"os"
	"strings"
)

// ParseArgs Parse command line arguments in format "key=value" into a hashmap
func ParseArgs() map[string]string {
	args := os.Args[1:] // Skip the program name (os.Args[0])
	argsMap := make(map[string]string)

	for _, arg := range args {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) == 2 {
			key := parts[0]
			value := parts[1]
			if key != "" && value != "" {
				argsMap[key] = value
			}
		}
	}

	return argsMap
}
