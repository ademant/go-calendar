VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

SERVICE    := dansal
BINDIR     := /usr/bin
SYSCONFDIR := /etc/dansal
STATEDIR   := /var/lib/dansal
SYSTEMDDIR := /lib/systemd/system

.PHONY: build build-dansal build-dansal_admin run clean install update deb

build: build-dansal build-dansal_admin

build-dansal:
	go build $(LDFLAGS) -o dansal ./cmd/dansal

build-dansal_admin:
	go build -o dansal_admin ./cmd/dansal_admin

run: build-dansal
	./dansal --config ./config.yaml

clean:
	rm -f dansal dansal_admin *.deb

install: build
	# system user
	id -u $(SERVICE) >/dev/null 2>&1 || \
		adduser --system --group --no-create-home --home-dir $(STATEDIR) \
		        --shell /usr/sbin/nologin $(SERVICE)
	# directories
	install -d -m 750 -o $(SERVICE) -g $(SERVICE) $(SYSCONFDIR)
	install -d -m 750 -o $(SERVICE) -g $(SERVICE) $(STATEDIR)
	install -d -m 750 -o $(SERVICE) -g $(SERVICE) $(STATEDIR)/images
	# config — install only if not already present so local edits are preserved
	@if [ ! -f $(SYSCONFDIR)/config.yaml ]; then \
		install -m 640 -o root -g $(SERVICE) packaging/config.yaml $(SYSCONFDIR)/config.yaml; \
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

# Build a .deb package. VERSION may be overridden by the CI pipeline
# (e.g.  make deb DEB_VERSION=0.1.0).
DEB_VERSION ?= $(shell git describe --tags --always 2>/dev/null | \
                sed 's/^v//; s/-\([0-9]*\)-g[0-9a-f]*/+\1/' | \
                grep -E '^[0-9]' || echo "0.0~git.$(shell date +%Y%m%d).$(shell git rev-parse --short HEAD 2>/dev/null)")
DEB_ARCH    ?= amd64
DEB_DIR     := /tmp/dansal-deb-$(shell date +%s%N)

deb: build-dansal build-dansal_admin
	# Package tree
	mkdir -p $(DEB_DIR)/DEBIAN
	mkdir -p $(DEB_DIR)/usr/bin
	mkdir -p $(DEB_DIR)/$(SYSTEMDDIR)
	mkdir -p $(DEB_DIR)/etc/dansal
	mkdir -p $(DEB_DIR)/etc/fail2ban/filter.d
	mkdir -p $(DEB_DIR)/etc/fail2ban/jail.d
	# Control files
	sed 's/VERSION_PLACEHOLDER/$(DEB_VERSION)/; s/amd64/$(DEB_ARCH)/' \
	    packaging/control > $(DEB_DIR)/DEBIAN/control
	cp  packaging/conffiles                          $(DEB_DIR)/DEBIAN/conffiles
	install -m 755 packaging/preinst                 $(DEB_DIR)/DEBIAN/preinst
	install -m 755 packaging/postinst                $(DEB_DIR)/DEBIAN/postinst
	install -m 755 packaging/prerm                   $(DEB_DIR)/DEBIAN/prerm
	install -m 755 packaging/postrm                  $(DEB_DIR)/DEBIAN/postrm
	# Binaries
	install -m 755 dansal                            $(DEB_DIR)/usr/bin/dansal
	install -m 755 dansal_admin                      $(DEB_DIR)/usr/bin/dansal_admin
	# Systemd unit
	install -m 644 dansal.service                    $(DEB_DIR)/$(SYSTEMDDIR)/dansal.service
	# Default config (with Debian-friendly paths)
	install -m 644 packaging/config.yaml             $(DEB_DIR)/etc/dansal/config.yaml
	# fail2ban (optional — installed but only activated when fail2ban is present)
	install -m 644 deploy/fail2ban/filter.d/dansal.conf $(DEB_DIR)/etc/fail2ban/filter.d/dansal.conf
	install -m 644 deploy/fail2ban/jail.d/dansal.conf   $(DEB_DIR)/etc/fail2ban/jail.d/dansal.conf
	# Build
	dpkg-deb --build --root-owner-group $(DEB_DIR) \
	    dansal_$(DEB_VERSION)_$(DEB_ARCH).deb
	rm -rf $(DEB_DIR)
	@echo "Built dansal_$(DEB_VERSION)_$(DEB_ARCH).deb"
