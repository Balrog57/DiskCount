#!/bin/bash
go clean -testcache
go test ./internal/parsing
