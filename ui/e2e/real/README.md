# Against the real directory

`npm run test:e2e` uses the mock. To run the `real/` specs against the Go
binary instead (Postgres, `DNS_VERIFIER=fake`, a fake auth issuer):

```sh
# Postgres, then the fake auth service (signs EdDSA tokens, serves JWKS)
docker run -d --rm --name dirweb-pg -e POSTGRES_USER=directory -e POSTGRES_PASSWORD=dirweb \
  -e POSTGRES_DB=directory -p 127.0.0.1:55460:5432 postgres:18-alpine
docker exec dirweb-pg psql -U directory -c 'create database directory2'
PORT=8790 node e2e/real/fake-auth.mjs &

# two directories (the second is imported as a peer); build with `make ui build` first
common="ENV=development AUTO_MIGRATE=true AUTH_ISSUER=http://localhost:8790 DNS_VERIFIER=fake \
  DIRECTORY_INCLUDE_MAIN=false HEALTH_WORKER_ENABLED=false VERIFY_WORKER_TICK=5s"
env $common DATABASE_URL='postgres://directory:dirweb@127.0.0.1:55460/directory?sslmode=disable' \
  HTTP_ADDR=:8460 INTERNAL_ADDR=:8461 DIRECTORY_PUBLIC_URL=http://localhost:8460 \
  DIRECTORY_NAME='Test directory' MEDIA_DIR=/tmp/dir1 ../bin/directory serve &
env $common DATABASE_URL='postgres://directory:dirweb@127.0.0.1:55460/directory2?sslmode=disable' \
  HTTP_ADDR=:8470 INTERNAL_ADDR=:8471 DIRECTORY_PUBLIC_URL=http://localhost:8470 \
  DIRECTORY_NAME='Friends directory' MEDIA_DIR=/tmp/dir2 ../bin/directory serve &

BASE_URL=http://localhost:8460 PEER_URL=http://localhost:8470 AUTH_URL=http://localhost:8790 npm run test:e2e
```

The specs seed both directories through the API and are safe to re-run.
