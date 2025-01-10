# Makefile

# Define the destination folder for the build
DEST_DIR := dist

# Define the binary name
BINARY_NAME := cli

# Build target
build:
	mkdir -p $(DEST_DIR)
	go build -o $(DEST_DIR)/$(BINARY_NAME)
