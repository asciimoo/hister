let ws;
let input = document.getElementById("search");
let results = document.getElementById("results");
let logo = document.getElementById("logo");
let emptyImg = "data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==";
let templates = {
    "result": document.getElementById("result"),
};

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
        console.log("Connected to WebSocket server");
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

function sendMessage() {
    let message = {"text": input.value, "highlight": "HTML"};
    ws.send(JSON.stringify(message));
}

function renderResults(event) {
    var d = JSON.parse(event.data);
    results.innerHTML = "";
    if(!d || !d.length) {
        if(!input.value) {
			logo.classList.remove("hidden");
            return
        }
        let u = "https://google.com/search?q="+escape(input.value);
        let n = createTemplate("result", {
            "a": (e) => { e.setAttribute("href", u); e.innerHTML = "No results found - search on Google"; e.classList.add("error"); },
            "span": (e) => { e.textContent = u; },
        });
        results.appendChild(n);
        return;
    }
    highlightIdx = 0;
    for(let i in d) {
		logo.classList.add("hidden");
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
    sendMessage();
});

let highlightIdx = 0;
window.addEventListener("keydown", function(e) {
    if(e.key == "Enter") {
        let url = document.querySelectorAll(".result a")[highlightIdx].getAttribute("href");
        window.open(url, "_blank");
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
        let u = "https://google.com/search?q="+escape(input.value);
        window.open(u, "_blank");
    }

});
