{{define "main"}}
<div class="search">
    <input type="text" autofocus placeholder="Search..." id="search" />
</div>
<div id="results" class="container"></div>
<div id="logo" class="title container">
<img src="/static/logo.png" />
</div>
<template id="result">
    <div class="result">
        <div class="result-title"><img><a></a></div>
        <span class="result-url"></span>
        <p class="result-content"></p>
    </div>
</template>
<input type="hidden" id="ws-url" value="{{ .Config.WebSocketURL }}" />
<script src="static/js/search.js"></script>
{{ end }}
