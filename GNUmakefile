.PHONY: db-migrate db-push install lint run-engineering test

prisma/schema.dev.prisma: prisma/schema.prisma
	sed -e 's/provider = "mysql"/provider = "sqlite"/' -e 's/  *@db\.[A-Za-z]*//g' $< > $@

db-migrate: node_modules
	NODE_ENV=production npx prisma migrate dev
	npx prisma generate

db-push: node_modules prisma/schema.dev.prisma
	npx prisma db push

node_modules: package.json package-lock.json
	npm install
	touch node_modules

install: node_modules

lint: node_modules
	npm run lint

run-engineering: db-push
	node --watch src/index.js

test: node_modules
	npm test
