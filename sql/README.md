# CLI Blog & RSS Aggregator

This is a real-time RSS feed aggregator operated entirely from the command line interface (CLI). Built with **Go**, it leverages **PostgreSQL** for data persistence and features a concurrent background worker engine that continuously discovers, cleans, and updates structured articles from news channels and blogs.

Because Go applications compile into self-contained static binaries, you can run the `gator` executable directly anywhere in your system after installation without requiring a Go development toolchain or runtime.

---

## Prerequisites

Before installing and running Gator, make sure you have the following installed on your machine:

* **Go** (version 1.20 or superior) -> [Download Go](https://go.dev/dl/)
* **PostgreSQL** -> [Download Postgres](https://www.postgresql.org/download/)

---

## Database Setup

1. Ensure your PostgreSQL service is up and running.
2. Create a new empty database named `gator`:
   ```bash
   psql -U postgres -c "CREATE DATABASE gator;"
3. Run migrations using your preferred schema management tool (such as goose) to set up the relational tables (users, feeds, feed_follows, posts).

## Installation

You can compile and install Gator directly from source using the Go toolchain. Open your terminal in the root directory of this repository and run:
Bash

```bash
go install .
```

This will automatically build the program and place the compiled executable binary into your system's $GOPATH/bin folder. Ensure this directory is present in your system's $PATH variable to execute the command globally.

## Initial Configuration

Gator depends on a local JSON configuration file located at your home directory (~/.gatorconfig.json).

Create this file manually or verify that its connection string points seamlessly to your target database instance. Example structure for ~/.gatorconfig.json:

```json
{
  "db_url": "postgres://postgres:postgres@localhost:5432/gator?sslmode=disable",
  "current_user_name": ""
}
```

## Command Reference

Once installed, you can interact with Gator by running the following CLI commands:

### User Management

Register a new user:
    
```bash
gator register <username>
```

Log in as an existing user:

```bash
gator login <username>
```

List all users in the database:

```bash
gator users
```

## Feeds & Subscriptions

Add a new RSS feed: (This automatically makes the current user follow it)

```bash
gator addfeed "Blog Name" "[https://url-to-feed.com/rss](https://url-to-feed.com/rss)"
```

List all registered feeds in the system:

```bash
gator feeds
```

Follow an existing feed:

```bash
gator follow <feed-url>
```

Unfollow a feed:

```bash
gator unfollow <feed-url>
```

List all feeds you are currently following:

```bash
gator following
```

## Aggregator & Reading Dashboard

Start the background scraping loop (Daemon Worker):
Run this command in a separate terminal window. It will scrape targeted feeds sequentially at the specified interval:

```bash
gator agg 1m
```

Browse collected articles: (Displays the newest posts from feeds you follow)

```bash
gator browse <optional_limit>
```

(Example: Run gator browse 5 to view your top 5 latest posts).

Reset the database: (Deletes all records from users, feeds, and follows)

```bash
gator reset
```