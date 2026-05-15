VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

SERVICE    := dansal
BINDIR     := /usr/local/bin
SYSCONFDIR := /etc/dansal
STATEDIR   := /var/lib/dansal
SYSTEMDDIR := /etc/systemd/system

.PHONY: build build-dansal build-web build-dansal_admin run clean install update

build: build-dansal build-web build-dansal_admin

build-dansal:
	go build $(LDFLAGS) -o dansal ./cmd/dansal

build-web:
	go build -o web ./cmd/web

build-dansal_admin:
	go build -o dansal_admin ./cmd/dansal_admin

run: build-dansal
	./dansal --config ./config.yaml

clean:
	rm -f dansal web dansal_admin

install: build
	# system user
	id -u $(SERVICE) >/dev/null 2>&1 || \
		useradd --system --no-create-home --home-dir $(STATEDIR) \
		        --shell /usr/sbin/nologin $(SERVICE)
	# directories
	install -d -m 750 -o $(SERVICE) -g $(SERVICE) $(SYSCONFDIR)
	install -d -m 750 -o $(SERVICE) -g $(SERVICE) $(STATEDIR)
	install -d -m 750 -o $(SERVICE) -g $(SERVICE) $(STATEDIR)/images
	# config — install only if not already present so local edits are preserved
	@if [ ! -f $(SYSCONFDIR)/config.yaml ]; then \
		sed \
			-e 's|images_dir: "./images"|images_dir: "$(STATEDIR)/images"|' \
			-e 's|admin_socket: "./dansal.sock"|admin_socket: "/run/$(SERVICE)/dansal.sock"|' \
			config.yaml > $(SYSCONFDIR)/config.yaml; \
		chown $(SERVICE):$(SERVICE) $(SYSCONFDIR)/config.yaml; \
		chmod 640 $(SYSCONFDIR)/config.yaml; \
		echo "Installed $(SYSCONFDIR)/config.yaml"; \
	else \
		echo "$(SYSCONFDIR)/config.yaml already exists — not overwriting"; \
	fi
	# binaries
	install -m 755 dansal        $(BINDIR)/dansal
	install -m 755 dansal_admin  $(BINDIR)/dansal_admin
	# systemd unit
	install -m 644 dansal.service $(SYSTEMDDIR)/dansal.service
	systemctl daemon-reload
	systemctl enable --now $(SERVICE)
	# fail2ban
	@if [ -d /etc/fail2ban ]; then \
		install -m 644 deploy/fail2ban/filter.d/dansal.conf /etc/fail2ban/filter.d/dansal.conf; \
		install -m 644 deploy/fail2ban/jail.d/dansal.conf   /etc/fail2ban/jail.d/dansal.conf; \
		systemctl reload fail2ban 2>/dev/null || true; \
		echo "Installed fail2ban filter and jail"; \
	else \
		echo "fail2ban not found — skipping (templates in deploy/fail2ban/)"; \
	fi

update: build
	install -m 755 dansal        $(BINDIR)/dansal
	install -m 755 dansal_admin  $(BINDIR)/dansal_admin
	systemctl restart $(SERVICE)
