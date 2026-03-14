.PHONY: db-migrate db-push deploy generate install lint run-engineering run-engineering-cron test

prisma/schema.dev.prisma: prisma/schema.prisma
	sed -e 's/provider = "mysql"/provider = "sqlite"/' -e 's/  *@db\.[A-Za-z]*//g' $< > $@

deploy:
	make -C ../bsmiweb-credentials decrypt > .env.deploy.tmp
	ansible-playbook -i ansible/inventory.ini ansible/deploy.yml -e dotenv_file=.env.deploy.tmp
	rm -f .env.deploy.tmp

db-migrate: node_modules
	NODE_ENV=production npx prisma migrate dev
	npx prisma generate

db-push: node_modules prisma/schema.dev.prisma
	npx prisma db push

node_modules: package.json package-lock.json
	npm install
	touch node_modules

install: node_modules

generate: node_modules prisma/schema.dev.prisma
	npx prisma generate --schema=prisma/schema.dev.prisma

lint: node_modules
	npm run lint

run-engineering: db-push
	node --watch src/index.js

run-engineering-cron: db-push
	node --watch src/cron.js

test: generate
	npm test
