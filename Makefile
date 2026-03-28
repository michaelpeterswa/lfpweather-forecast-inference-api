# Makefile
all: setup hooks

# requires `nvm use --lts` or `nvm use node`
.PHONY: setup
setup: 
	npm install -g @commitlint/config-conventional @commitlint/cli  


.PHONY: hooks
hooks:
	@git config --local core.hooksPath .githooks/

API_KEY ?= f791709e0fc2a4eabfdca42a50d905a8
BASE_URL ?= http://localhost:8080

.PHONY: forecast-summary
forecast-summary:
	curl -s -H "X-API-Key: $(API_KEY)" $(BASE_URL)/api/v1/forecast/summary | jq .

.PHONY: forecast-detailed
forecast-detailed:
	curl -s -H "X-API-Key: $(API_KEY)" $(BASE_URL)/api/v1/forecast/detailed | jq .