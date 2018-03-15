package view

import (
        html "html/template"
)

const style = `
{{define "style"}}
<style>
  .col {
    border: 1px solid;
  }
  .cell {
    width: 30px;
    height: 30px;
    text-align: center;
    line-height: 30px;
  }
  .win {
    background: #ABF095;
  }
  .loss {
    background: #F7C1C1;
  }
  .mine {
    background: #C1EAF7;
  }
  .yours {
    background: #E6E3E3;
  }
  #settings, #players {
    font-size: 0.8em;
  }
</style>
{{end}}
`

var RootTemplate = html.Must(html.New("root").Parse(style+rootTemplateHTML))
const rootTemplateHTML = `
{{define "root"}}
<html>
  <head>
  {{template "style"}}
  </head>
  <body>
    <h3>K-D Tic Tac Toe</h3>
    <p>Choose a game style below:</p>
    <ul>
      <li><a href="/game?handle=Player 1;playerCount=2;k=2;size=3;inarow=3">Classic (2D-2P)</a></li>
      <li><a href="/game?handle=Player 1;playerCount=2;k=3;size=4;inarow=4">Deep Thinker (3D-2P)</a></li>
      <li><a href="/game?handle=Player 1;playerCount=4;k=4;size=5;inarow=5">Preposterous Party (4D-4P)</a></li>
      <li><a href="/new">Custom</a></li>
    </ul>
  </body>
</html>
{{end}}
`

var NewGameTemplate = html.Must(html.New("getnew").Parse(style+newGameTemplateHTML))
const newGameTemplateHTML = `
{{define "getnew"}}
<html>
  <head>
  {{template "style"}}
  </head>
  <body>
    <h3>New Game</h3>
    <p>{{.}}</p>
    <form action="/game" method="post">
      <div>Handle: <input type="text" name="handle"></div>
      <div>PlayerCount: <input type="text" name="playerCount"></div>
      <div>K: <input type="text" name="k"></div>
      <div>Size: <input type="text" name="size"></div>
      <div>In a row: <input type="text" name="inarow"></div>
      <div><input type="submit" value="Create"></div>
    </form>
  </body>
</html>
{{end}}
`

var PostGameTemplate = html.Must(html.New("postgame").Parse(style+postGameTemplateHTML))
const postGameTemplateHTML = `
{{define "postgame"}}
{{ $inARow := .Rules.InARow }}
<html>
  <head>
  {{template "style"}}
  </head>
  <h3>New Game</h3>
  <body>
    <p>Share these links to play:</p>
    <ol>
    {{range .Players}}<li><a href="game/{{.GameId}}?player={{.PlayerId}};message=Game on! Get {{ $inARow }} in a row to win.">{{.Handle}}</a></li>{{end}}
    </ol>
  </body>
</html>
{{end}}
`

var GetGameTemplate = html.Must(html.New("getgame").Parse(style+getGameTemplateHTML))
const getGameTemplateHTML = `
{{define "getgame"}}
<html>
  <head>
  {{template "style"}}
  </head>
  <body>
  {{if .HasToken}}
    <script type="text/javascript" src="/_ah/channel/jsapi"></script>
    <script>
      channel = new goog.appengine.Channel('{{.Token}}');
      socket = channel.open();
      socket.onmessage = function() { location.reload(); };
    </script>
  {{end}}
    {{if .HasViewer}}<h3>{{.Viewer.Handle}}{{ if .IsMyTurn }} (your turn){{end}}</h3>
    {{else}}<h3>{{.GameId}}</h3>{{end}}
    {{if .Won}}<p>Game over!</p>{{else}}<p>{{.Message}}</p>{{end}}
    <div>{{.View}}</div>
    <div id="players">
      <h4>Players:</h4>
      {{.PlayerList}}
    </div>
    {{if .HasViewer}}
      <div id="settings">
        <h4>Settings:</h4>
        <ul><li><a href="/settings/{{.GameId}}?player={{.Viewer.PlayerId}}">Change my handle</a></li></ul>
      </div>
    {{end}}
  </body>
</html>
{{end}}
`

var GetSettingsTemplate = html.Must(html.New("getsettings").Parse(style+getSettingsTemplateHTML))
const getSettingsTemplateHTML = `
{{define "getsettings"}}
<html>
  <head>
  {{template "style"}}
  </head>
  <body>
    <h3>Player Settings</h3>
    <form action="/player/{{.GameId}}?player={{.PlayerId}}" method="post">
      <div>Handle: <input type="text" name="handle" value="{{.Handle}}"></div>
      <div><input type="submit" value="Change"></div>
    </form>
  </body>
</html>
{{end}}
`
