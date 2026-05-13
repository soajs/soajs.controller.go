#!/bin/bash

# Start the Go controller for local development
# This mirrors the configuration used by Node.js services via index_local.js

export SOAJS_ENV="dashboard"
export SOAJS_MONGO_URI="mongodb://127.0.0.1:32017"

# Optional: Additional environment variables
# export SOAJS_LOG_LEVEL="debug"

cd "$(dirname "$0")"
./controller "$@"
