package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iangmartins/blog-aggregator-go/internal/config"
	"github.com/iangmartins/blog-aggregator-go/internal/database"
	_ "github.com/lib/pq"
)

type state struct {
	db  *database.Queries
	cfg *config.Config
}

type command struct {
	name string
	args []string
}

type commands struct {
	handlers map[string]func(*state, command) error
}

func (c *commands) register(name string, f func(*state, command) error) {
	c.handlers[name] = f
}

func (c *commands) run(s *state, cmd command) error {
	handler, exists := c.handlers[cmd.name]
	if !exists {
		return fmt.Errorf("unknown command: %s", cmd.name)
	}
	return handler(s, cmd)
}

func main() {
	cfg, err := config.Read()
	if err != nil {
		log.Fatalf("error reading config: %v", err)
	}

	db, err := sql.Open("postgres", cfg.DbURL)
	if err != nil {
		log.Fatalf("error opening database: %v", err)
	}
	defer db.Close()

	dbQueries := database.New(db)

	appState := &state{
		db:  dbQueries,
		cfg: &cfg,
	}

	cmds := commands{
		handlers: make(map[string]func(*state, command) error),
	}
	cmds.register("login", handlerLogin)
	cmds.register("register", handlerRegister)
	cmds.register("reset", handlerReset)
	cmds.register("users", handlerUsers)
	cmds.register("agg", handlerAgg)
	cmds.register("feeds", handlerFeeds)

	cmds.register("addfeed", middlewareLoggedIn(handlerAddFeed))
	cmds.register("follow", middlewareLoggedIn(handlerFollow))
	cmds.register("following", middlewareLoggedIn(handlerFollowing))
	cmds.register("unfollow", middlewareLoggedIn(handlerUnfollow))
	cmds.register("browse", middlewareLoggedIn(handlerBrowse))

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "error: not enough arguments provided")
		os.Exit(1)
	}

	cmdName := os.Args[1]
	cmdArgs := os.Args[2:]

	cmd := command{
		name: cmdName,
		args: cmdArgs,
	}

	err = cmds.run(appState, cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func handlerRegister(s *state, cmd command) error {
	if len(cmd.args) == 0 {
		return errors.New("the register handler expects a single argument (username)")
	}

	username := cmd.args[0]

	ctx := context.Background()

	params := database.CreateUserParams{
		ID:        uuid.New(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Name:      username,
	}

	user, err := s.db.CreateUser(ctx, params)
	if err != nil {
		return fmt.Errorf("could not create user: %w", err)
	}

	err = s.cfg.SetUser(user.Name)
	if err != nil {
		return fmt.Errorf("user created but failed to update local config: %w", err)
	}

	fmt.Printf("User was successfully created!\n")
	fmt.Printf("Debug info: ID: %v | Name: %s | CreatedAt: %v\n", user.ID, user.Name, user.CreatedAt)
	return nil
}

func handlerLogin(s *state, cmd command) error {
	if len(cmd.args) == 0 {
		return errors.New("the login handler expects a single argument (username)")
	}

	username := cmd.args[0]
	ctx := context.Background()

	_, err := s.db.GetUser(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("user '%s' does not exist - you cannot login", username)
		}
		return fmt.Errorf("failed looking up user: %w", err)
	}

	err = s.cfg.SetUser(username)
	if err != nil {
		return fmt.Errorf("could not update login context: %w", err)
	}

	fmt.Printf("User has been logged in as: %s\n", username)
	return nil
}

func handlerReset(s *state, cmd command) error {
	ctx := context.Background()

	err := s.db.ResetUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to reset database: %w", err)
	}

	fmt.Println("Database successfully reset! All user records have been cleared.")
	return nil
}

func handlerUsers(s *state, cmd command) error {
	ctx := context.Background()

	users, err := s.db.GetUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to retrieve users: %w", err)
	}

	for _, user := range users {
		if user.Name == s.cfg.CurrentUserName {
			fmt.Printf("* %s (current)\n", user.Name)
		} else {
			fmt.Printf("* %s\n", user.Name)
		}
	}

	return nil
}

func handlerAgg(s *state, cmd command) error {
	if len(cmd.args) < 1 {
		return errors.New("the agg handler expects a single argument: time_between_reqs (e.g. 1m, 10s, 1h)")
	}

	timeBetweenRequests, err := time.ParseDuration(cmd.args[0])
	if err != nil {
		return fmt.Errorf("invalid duration format: %w", err)
	}

	fmt.Printf("Collecting feeds every %v\n", timeBetweenRequests)

	ticker := time.NewTicker(timeBetweenRequests)
	for ; ; <-ticker.C {
		err := scrapeFeeds(s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Scraper warning/error: %v\n", err)
		}
	}
}

func handlerAddFeed(s *state, cmd command, user database.User) error {
	if len(cmd.args) < 2 {
		return errors.New("the addfeed handler expects two arguments: name and url")
	}

	feedName := cmd.args[0]
	feedURL := cmd.args[1]
	ctx := context.Background()

	params := database.CreateFeedParams{
		ID:        uuid.New(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Name:      feedName,
		Url:       feedURL,
		UserID:    user.ID,
	}

	feed, err := s.db.CreateFeed(ctx, params)
	if err != nil {
		return fmt.Errorf("could not create feed: %w", err)
	}

	followParams := database.CreateFeedFollowParams{
		ID:        uuid.New(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		UserID:    user.ID,
		FeedID:    feed.ID,
	}

	_, err = s.db.CreateFeedFollow(ctx, followParams)
	if err != nil {
		return fmt.Errorf("feed created, but failed to automatically follow it: %w", err)
	}

	fmt.Println("Feed successfully created!")
	fmt.Printf("* ID:         %s\n", feed.ID)
	fmt.Printf("* Created At: %v\n", feed.CreatedAt)
	fmt.Printf("* Updated At: %v\n", feed.UpdatedAt)
	fmt.Printf("* Name:       %s\n", feed.Name)
	fmt.Printf("* URL:        %s\n", feed.Url)
	fmt.Printf("* User ID:    %s\n", feed.UserID)

	return nil
}
func handlerFeeds(s *state, cmd command) error {
	ctx := context.Background()

	feeds, err := s.db.GetFeeds(ctx)
	if err != nil {
		return fmt.Errorf("failed to retrieve feeds: %w", err)
	}

	if len(feeds) == 0 {
		fmt.Println("No feeds found in the database.")
		return nil
	}

	for _, feed := range feeds {
		fmt.Printf("* Name:    %s\n", feed.Name)
		fmt.Printf("  URL:     %s\n", feed.Url)
		fmt.Printf("  Created By: %s\n", feed.UserName)
		fmt.Println(strings.Repeat("-", 20))
	}

	return nil
}
func handlerFollow(s *state, cmd command, user database.User) error {
	if len(cmd.args) == 0 {
		return errors.New("the follow handler expects a single argument: feed_url")
	}

	feedURL := cmd.args[0]
	ctx := context.Background()

	feed, err := s.db.GetFeedByURL(ctx, feedURL)
	if err != nil {
		return fmt.Errorf("feed not found with the provided URL: %w", err)
	}

	params := database.CreateFeedFollowParams{
		ID:        uuid.New(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		UserID:    user.ID,
		FeedID:    feed.ID,
	}

	ff, err := s.db.CreateFeedFollow(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to follow feed: %w", err)
	}

	fmt.Printf("Successfully followed feed!\n")
	fmt.Printf("* User: %s\n", ff.UserName)
	fmt.Printf("* Feed: %s\n", ff.FeedName)

	return nil
}

func handlerFollowing(s *state, cmd command, user database.User) error {
	ctx := context.Background()

	follows, err := s.db.GetFeedFollowsForUser(ctx, user.ID) // Provided by middleware!
	if err != nil {
		return fmt.Errorf("failed to fetch feed follows: %w", err)
	}

	if len(follows) == 0 {
		fmt.Println("You are not following any feeds yet.")
		return nil
	}

	fmt.Printf("Feeds followed by %s:\n", user.Name)
	for _, f := range follows {
		fmt.Printf("* %s\n", f.FeedName)
	}

	return nil
}

func middlewareLoggedIn(handler func(s *state, cmd command, user database.User) error) func(*state, command) error {
	return func(s *state, cmd command) error {
		ctx := context.Background()

		user, err := s.db.GetUser(ctx, s.cfg.CurrentUserName)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("current user '%s' does not exist in the database, please register or login first", s.cfg.CurrentUserName)
			}
			return fmt.Errorf("failed to fetch current user: %w", err)
		}

		return handler(s, cmd, user)
	}
}

func handlerUnfollow(s *state, cmd command, user database.User) error {
	if len(cmd.args) == 0 {
		return errors.New("the unfollow handler expects a single argument: feed_url")
	}

	feedURL := cmd.args[0]
	ctx := context.Background()

	feed, err := s.db.GetFeedByURL(ctx, feedURL)
	if err != nil {
		return fmt.Errorf("could not find a feed with the provided URL: %w", err)
	}

	params := database.DeleteFeedFollowParams{
		UserID: user.ID,
		FeedID: feed.ID,
	}

	err = s.db.DeleteFeedFollow(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to unfollow feed: %w", err)
	}

	fmt.Printf("Successfully unfollowed feed: %s\n", feed.Name)
	return nil
}

func scrapeFeeds(s *state) error {
	ctx := context.Background()

	feedRecord, err := s.db.GetNextFeedToFetch(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("no feeds discovered in the database to scrape")
		}
		return fmt.Errorf("failed fetching priority queue item: %w", err)
	}

	_, err = s.db.MarkFeedFetched(ctx, feedRecord.ID)
	if err != nil {
		return fmt.Errorf("failed to mark feed updated state: %w", err)
	}

	fmt.Printf("\n[Scraper] Fetching feed '%s' at URL: %s...\n", feedRecord.Name, feedRecord.Url)
	rssData, err := fetchFeed(ctx, feedRecord.Url)
	if err != nil {
		return fmt.Errorf("network resource exception on '%s': %w", feedRecord.Name, err)
	}

	fmt.Printf("Found %d articles inside feed. Saving to database...\n", len(rssData.Channel.Item))
	for _, item := range rssData.Channel.Item {
		publishedAt, err := parsePublishedAt(item.PubDate)
		if err != nil {
			fmt.Printf("Warning: couldn't parse timestamp for post '%s': %v\n", item.Title, err)
			publishedAt = time.Now().UTC()
		}

		var description sql.NullString
		if item.Description != "" {
			description = sql.NullString{String: item.Description, Valid: true}
		}

		params := database.CreatePostParams{
			ID:          uuid.New(),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
			Title:       item.Title,
			Url:         item.Link,
			Description: description,
			PublishedAt: publishedAt,
			FeedID:      feedRecord.ID,
		}

		_, err = s.db.CreatePost(ctx, params)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
				continue
			}
			fmt.Printf("Error saving post '%s': %v\n", item.Title, err)
		}
	}

	return nil
}

func handlerBrowse(s *state, cmd command, user database.User) error {
	limit := 2
	if len(cmd.args) > 0 {
		parsedLimit, err := strconv.Atoi(cmd.args[0])
		if err != nil {
			return fmt.Errorf("invalid limit parameter (must be an integer): %w", err)
		}
		if parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	ctx := context.Background()
	posts, err := s.db.GetPostsForUser(ctx, database.GetPostsForUserParams{
		UserID: user.ID,
		Limit:  int32(limit),
	})
	if err != nil {
		return fmt.Errorf("failed to retrieve posts: %w", err)
	}

	if len(posts) == 0 {
		fmt.Println("No posts found. Are you following active feeds that have been scraped?")
		return nil
	}

	fmt.Printf("Displaying top %d posts for user %s:\n\n", len(posts), user.Name)
	for _, post := range posts {
		fmt.Printf("=== %s ===\n", post.Title)
		fmt.Printf("Published: %v\n", post.PublishedAt.Format(time.RFC1123))
		fmt.Printf("Link:      %s\n", post.Url)
		if post.Description.Valid && post.Description.String != "" {
			fmt.Printf("Summary:   %s\n", post.Description.String)
		}
		fmt.Println()
	}

	return nil
}
