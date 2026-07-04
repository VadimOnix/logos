# OpenWrt package feed Makefile for logos-agent.
#
# Usage: add this repository as a feed, or copy agent/openwrt into
# package/logos-agent inside an OpenWrt buildroot/SDK checkout:
#
#   ./scripts/feeds update -a && ./scripts/feeds install logos-agent
#   make package/logos-agent/compile
#
# Targets OpenWrt 23.05 (opkg) and 24.10+ (apk); both consume this format.

include $(TOPDIR)/rules.mk

PKG_NAME:=logos-agent
PKG_VERSION:=0.1.0
PKG_RELEASE:=1

PKG_SOURCE_PROTO:=git
PKG_SOURCE_URL:=https://github.com/VadimOnix/logos.git
PKG_SOURCE_VERSION:=master
PKG_MIRROR_HASH:=skip

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16

GO_PKG:=github.com/VadimOnix/logos/agent
GO_PKG_BUILD_PKG:=github.com/VadimOnix/logos/agent/cmd/logos-agent
GO_PKG_LDFLAGS:=-s -w
GO_PKG_LDFLAGS_X:=github.com/VadimOnix/logos/agent/internal/agent.Version=$(PKG_VERSION)

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

define Package/logos-agent
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Logos fleet-management agent
  URL:=https://github.com/VadimOnix/logos
  DEPENDS:=$(GO_ARCH_DEPENDS) +ca-bundle
endef

define Package/logos-agent/description
  Agent for the Logos control plane: outbound management channel,
  heartbeat metrics, package management, and UCI config access.
  Enroll with: logos-agent enroll --server <url> --code <claim-code>
endef

define Package/logos-agent/conffiles
/etc/logos/agent.json
endef

define Package/logos-agent/install
	$(call GoPackage/Package/Install/Bin,$(1))
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/logos-agent.init $(1)/etc/init.d/logos-agent
endef

$(eval $(call GoBinPackage,logos-agent))
$(eval $(call BuildPackage,logos-agent))
