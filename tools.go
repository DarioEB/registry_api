//go:build tools

package main

// This file retains module dependencies that are not yet imported in code.
// They will be used in subsequent stories. Remove this file once all
// packages are imported in real application code.

import (
	_ "github.com/gin-contrib/cors"
	_ "github.com/golang-jwt/jwt/v5"
)
