#!/bin/sh

# Update desktop database for .desktop file changes
# This makes the application appear in application menus and registers its capabilities.
if command -v update-desktop-database >/dev/null 2>&1; then
  echo "Updating desktop database..."
  update-desktop-database -q /usr/share/applications
else
  echo "Warning: update-desktop-database command not found. Desktop file may not be immediately recognized." >&2
fi

# Update MIME database for custom URL schemes (x-scheme-handler)
# This ensures the system knows how to handle your custom protocols.
if command -v update-mime-database >/dev/null 2>&1; then
  echo "Updating MIME database..."
  update-mime-database -n /usr/share/mime
else
  echo "Warning: update-mime-database command not found. Custom URL schemes may not be immediately recognized." >&2
fi

# Load the AppArmor profile that lets WebKitGTK's bubblewrap sandbox create
# user namespaces on kernels that restrict them (Ubuntu 23.10+). On systems
# without AppArmor (e.g. Fedora) this is skipped and the file is inert.
if command -v apparmor_parser >/dev/null 2>&1 && [ -f /etc/apparmor.d/tracker ]; then
  apparmor_parser -r /etc/apparmor.d/tracker || true
fi

exit 0
