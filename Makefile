# Build and install playcall. `make` builds both programs in this folder;
# `make install` copies them to $(PREFIX)/bin (default ~/.local/bin, so no sudo).
# The GUI needs cgo and Fyne's system libraries (see the README); `make cli` builds
# only the command-line program, which needs nothing but Go.

GO      ?= go
PREFIX  ?= $(HOME)/.local
BINDIR  ?= $(PREFIX)/bin

.PHONY: all build cli gui install install-cli uninstall test clean

all: build

build: cli gui

cli:
	$(GO) build -trimpath -o playcall ./cmd/playcall

gui:
	$(GO) build -trimpath -o playcall-gui ./cmd/playcall-gui

install: build
	install -d $(DESTDIR)$(BINDIR)
	install -m 755 playcall playcall-gui $(DESTDIR)$(BINDIR)

install-cli: cli
	install -d $(DESTDIR)$(BINDIR)
	install -m 755 playcall $(DESTDIR)$(BINDIR)

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/playcall $(DESTDIR)$(BINDIR)/playcall-gui

test:
	$(GO) vet ./...
	$(GO) test ./...

clean:
	rm -f playcall playcall-gui
