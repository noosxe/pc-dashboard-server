FROM mcr.microsoft.com/devcontainers/go:1.26

# Install dolt for beads remote sync and management
RUN curl -L https://github.com/dolthub/dolt/releases/latest/download/install.sh | bash

# Install sqlite3 for local development
RUN apt-get update && export DEBIAN_FRONTEND=noninteractive \
    && apt-get -y install --no-install-recommends sqlite3 build-essential curl

RUN curl -fsSL https://antigravity.google/cli/install.sh | bash

# Ensure GOPATH/bin is in PATH for both root and vscode user
ENV GOPATH="/go"
ENV PATH="${GOPATH}/bin:/home/vscode/go/bin:${PATH}"

# Copy and run the tools installation script
USER vscode
COPY --chown=vscode:vscode scripts/install-tools.sh /tmp/scripts/install-tools.sh
RUN chmod +x /tmp/scripts/install-tools.sh && /tmp/scripts/install-tools.sh && rm -rf /tmp/scripts/install-tools.sh
USER root
