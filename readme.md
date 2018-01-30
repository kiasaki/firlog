## firlog

_Log collector server that supports Heroku's drain API. Backed by [Bleve](https://github.com/blevesearch/bleve)._

### intro

firlog is a very simplistic log aggregation service for small applications.
It currently supports Heroku drains as input and offers a basic search interface
secured by basic authentication.

![search interface](https://raw.githubusercontent.com/kiasaki/firlog/master/screenshot.png)

### running

```
$ go get github.com/kiasaki/firlog/cmd/firlog
$ firlog -dataDir /mnt/data/firlog -port 3000 -basic-auth user:pass -tokens app1-02s8b6kq8cf61v5hjz1,app2-...
2002/12/25 08:00:00 started listening on port 3000
```

Where

- **-data-dir** (or env var DATA_DIR) (default "data") is the directory all the bleve indexes will be stored in
- **-port** (or env var PORT) (default "3000") is the port you want to app to listen on
- **-basic-auth** (or env var BASIC_AUTH) is a "username:password" pair used to access the search UI
- **-tokens** (or env var TOKENS) is a comma delimited list of tokens used to authenticate bulk insert requests

### configuring heroku drains

As simple as

```
$ heroku drains:add http://<FIRLOG-HOSTNAME>/bulk/<INSERT-TOKEN-HERE> -a myapp
```

### license

MIT. See `LICENSE` file.
