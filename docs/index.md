---
layout: home

hero:
  name: Power Hour Generator
  text: Build power hours from the command line
  tagline: A Go CLI that orchestrates yt-dlp and ffmpeg to produce power hour video clip libraries from CSV plans
  actions:
    - theme: brand
      text: Get Started
      link: /guide/getting-started
    - theme: alt
      text: Project Board
      link: /board

features:
  - title: CSV-Driven Workflow
    details: Define your power hour in a simple CSV/TSV file with titles, artists, timestamps, and links. The CLI handles the rest.
  - title: Automatic Caching
    details: Source videos are downloaded once and cached locally. Re-renders skip downloads, and cache integrity is tracked via index metadata.
  - title: Configurable Overlays
    details: Profile-based overlay system with title cards, artist credits, index badges, and custom segments — all with independent fade timing.
  - title: Collections System
    details: Organize songs, interstitials, bumpers, and outros as independent collections with custom CSV headers and output directories.
---
