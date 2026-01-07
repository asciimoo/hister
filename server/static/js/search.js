let ws;
let input = document.getElementById("search");
let results = document.getElementById("results");
let resultsHeader = document.getElementById("results-header");
let emptyImg = "data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==";
let templates = {
    "result": document.getElementById("result"),
};
let urlState = {};

function createTemplate(name, fns) {
    let el = document.importNode(templates[name].content, true)
    for(let k in fns) {
        fns[k](el.querySelector(k));
    }
    return el;
}

function connect() {
    ws = new WebSocket(document.querySelector("#ws-url").value);

    ws.onopen = function() {
        const urlParams = new URLSearchParams(window.location.search);
		const query = urlParams.get('q');
        if(query) {
            sendQuery(query);
            input.value = query;
        }
    };

    ws.onmessage = renderResults;

    ws.onclose = function() {
        console.log("WebSocket connection closed, retrying...");
        setTimeout(connect, 1000); // Reconnect after 1 second
    };

    ws.onerror = function(error) {
        console.error("WebSocket error:", error);
    };
}

function sendQuery(q) {
    let message = {"text": q, "highlight": "HTML"};
    ws.send(JSON.stringify(message));
}

function updateURL() {
    if(input.value) {
        history.replaceState(urlState, "", `${window.location.pathname}?q=${encodeURIComponent(input.value)}`);
        return;
    }
    history.replaceState(urlState, "", `${window.location.pathname}`);
}

function renderResults(event) {
    resultsHeader.classList.add("hidden");
    const res = JSON.parse(event.data);
    results.innerHTML = "";
    resultsHeader.querySelector(".expanded-query").innerHTML = "";
    const d = res.documents;
    if(!d || !d.length) {
        if(!input.value) {
            return
        }
        let u = getSearchUrl(input.value)
        let n = createTemplate("result", {
            "a": (e) => { e.setAttribute("href", u); e.innerHTML = "No results found - open query in web search engine"; e.classList.add("error"); },
            "span": (e) => { e.textContent = u; },
        });
        results.appendChild(n);
        return;
    }
    highlightIdx = 0;
    resultsHeader.querySelector(".results-num").innerText = res.total;
    resultsHeader.classList.remove("hidden");
    if(res.query.text != input.value) {
        resultsHeader.querySelector(".expanded-query").innerHTML = `Expanded query: <code>"${res.query.text}"</code>`;
    }
    for(let i in d) {
        let r = d[i];
        let n = createTemplate("result", {
            "a": (e) => { e.setAttribute("href", r.url); e.innerHTML = r.title || "*title*"; },
            "img": (e) => { e.setAttribute("src", r.favicon || emptyImg); },
            "span": (e) => { e.textContent = r.url; },
            "p": (e) => { e.innerHTML = r.text; },
        });
        results.appendChild(n);
    }
};

connect();

input.addEventListener("input", () => {
    updateURL();
    sendQuery(input.value);
});

function getSearchUrl(query) {
    return document.querySelector("#search-url").value.replace("{query}", escape(query));
}

function openUrl(u) {
    window.location.href = u;
}

let highlightIdx = 0;
window.addEventListener("keydown", function(e) {
    if(e.key == "Enter") {
        let url = document.querySelectorAll(".result a")[highlightIdx].getAttribute("href");
        openUrl(url);
    }
    if(e.ctrlKey && (e.key == "j" || e.key == "k")) {
          e.preventDefault();
          if(results.children.length > highlightIdx) {
              results.children[highlightIdx].classList.remove("highlight");
          }
          highlightIdx = (highlightIdx+(e.key=="j"?1:-1)+results.children.length) % results.children.length;
          results.children[highlightIdx].classList.add("highlight");
    }
    if(e.ctrlKey && e.key == "o") {
        e.preventDefault();
        openUrl(getSearchUrl(input.value));
    }
});
