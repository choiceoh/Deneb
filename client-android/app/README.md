# Deneb Native App

This directory contains the vendored native client used by Deneb.

The app keeps the Compose chat UI and interactive `kai-ui` renderer from the
upstream Android project, then routes chat, memory, tools, scheduling, and
background work through the Deneb gateway.

For build and integration notes, see `../README.md`.
