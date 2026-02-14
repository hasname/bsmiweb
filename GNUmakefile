.PHONY: install lint run-engineering test

node_modules: package.json package-lock.json
	npm install
	touch node_modules

install: node_modules

lint: node_modules
	npm run lint

run-engineering: node_modules
	npm start

test: node_modules
	npm test
