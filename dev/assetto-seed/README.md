# Fake Assetto Corsa installation (dev only)

This folder is copied into the server manager's `assetto/` install path for local development.
Because `system/` exists, server manager skips the steamcmd install step, so no Steam account
or real `acServer` binary is needed. It contains a couple of fake cars and tracks so that the
race/championship forms have something to select. Starting an actual race will fail, as there
is no `acServer` executable.
