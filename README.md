# Hister

**Web history on steroids**

Hister is a web history management tool that provides blazing fast, content-based search for visited websites. Unlike traditional browser history that only searches URLs and titles, Hister indexes the full content of web pages you visit, enabling deep and meaningful search across your browsing history.

![hister screencast](assets/demo.gif)


## Features

- **Privacy-focused**: Keep your browsing history indexed locally - don't use remote search engines if it isn't necessary
- **Full-text indexing**: Search through the actual content of web pages you've visited
- **Advanced search capabilities**: Utilize a powerful [query language](https://blevesearch.com/docs/Query-String-Query/) for precise results
- **Efficient retrieval**: Use keyword aliases to quickly find content
- **Flexible content management**: Configure blacklist and priority rules for better control

## Setup & run

 - Clone the repository
 - Build with `go build`
 - Run `./hister help` to list the available commands
 - Execute `./hister listen` to start the web application
 - Install the extension: [Chrome](https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb), [Firefox](https://addons.mozilla.org/en-US/firefox/addon/hister/)


## Configuration

Settings can be configured in `~/.config/hister/config.yml` config file - don't forget to restart webapp after updating.

Execute `./hister create-config config.yml` to generate a configuration file with the default configuration values.


## Bugs

Bugs or suggestions? Visit the [issue tracker](https://github.com/asciimoo/hister/issues).


## License

AGPLv3
