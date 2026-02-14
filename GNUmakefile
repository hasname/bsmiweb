.PHONY: db-migrate db-push install lint run-engineering test

db-migrate: node_modules
	NODE_ENV=production npx prisma migrate dev
	npx prisma generate

db-push: node_modules
	npx prisma db push

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
