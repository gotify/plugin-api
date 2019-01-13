PKG=./

cap-version:
	wget -O /tmp/gotify.go.mod https://github.com/gotify/server/raw/master/go.mod; \
	for P in ${PKG}; do \
		go run github.com/gotify/plugin-api/cmd/gomod-cap -to `echo $$P` -from /tmp/gotify.go.mod; \
	done

check: check-go check-symbol check-mod check-lint

check-go:
	for P in ${PKG}; do \
		go test `echo $$P`; \
	done

check-lint:
	for P in ${PKG}; do \
		golint -set_exit_status `echo $$P`; \
	done

check-mod:
	wget -O /tmp/gotify.go.mod https://github.com/gotify/server/raw/master/go.mod; \
	for P in ${PKG}; do \
		go run github.com/gotify/plugin-api/cmd/gomod-cap -to `echo $$P` -from /tmp/gotify.go.mod -check=true; \
	done

check-symbol:
	for P in ${PKG}; do \
		go-exports -d `echo $$P` -c `echo $$P/export_ref_do_not_edit.json`; \
	done

release: generate-symbol

generate-symbol:
	for P in ${PKG}; do \
		if [ ! -f `echo $$P/export_ref_do_not_edit.json` ]; then \
			go-exports -d `echo $$P` > `echo $$P/export_ref_do_not_edit.json`; \
		fi; \
	done

download-tools:
	go get -u golang.org/x/lint/golint
	go get -u github.com/eternal-flame-AD/go-exports

.PHONY: check-go check-symbol check-lint check-mod generate-symbol cap-version download-tools