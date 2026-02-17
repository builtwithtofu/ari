FROM mcr.microsoft.com/devcontainers/javascript-node:22

RUN apt-get update \
  && apt-get install -y --no-install-recommends curl ca-certificates \
  && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://bun.sh/install | bash \
  && ln -sf /root/.bun/bin/bun /usr/local/bin/bun \
  && ln -sf /root/.bun/bin/bunx /usr/local/bin/bunx

ENV BUN_INSTALL=/root/.bun
ENV PATH=/root/.bun/bin:${PATH}

RUN bun add -g opencode-ai@1.2.6
