package wfruntime

// prelude defines the standard globals on top of the private
// __autable_native helpers, which are removed afterwards.
const prelude = `(function (g, native) {
  "use strict";
  function define(name, value) {
    Object.defineProperty(g, name, { value: value, writable: true, configurable: true, enumerable: false });
  }

  class TextEncoder {
    get encoding() { return "utf-8"; }
    encode(input) { return new Uint8Array(native.utf8Encode(input === undefined ? "" : String(input))); }
  }
  class TextDecoder {
    constructor(label) {
      const normalized = label === undefined ? "utf-8" : String(label).trim().toLowerCase();
      if (normalized !== "utf-8" && normalized !== "utf8" && normalized !== "unicode-1-1-utf-8") {
        throw new RangeError("TextDecoder only supports utf-8, got " + label);
      }
    }
    get encoding() { return "utf-8"; }
    decode(input) {
      if (input === undefined) return "";
      let text = native.utf8Decode(input);
      if (text.charCodeAt(0) === 0xfeff) text = text.slice(1);
      return text;
    }
  }
  define("TextEncoder", TextEncoder);
  define("TextDecoder", TextDecoder);
  define("atob", function atob(data) { return native.atob(String(data)); });
  define("btoa", function btoa(data) { return native.btoa(String(data)); });

  function formEncode(text) {
    return encodeURIComponent(text).replace(/[!'()~]/g, function (c) {
      return "%" + c.charCodeAt(0).toString(16).toUpperCase();
    }).replace(/%20/g, "+");
  }
  function formDecode(text) {
    return decodeURIComponent(text.replace(/\+/g, " "));
  }

  const listKey = Symbol("list");
  const ownerKey = Symbol("owner");
  class URLSearchParams {
    constructor(init) {
      this[listKey] = [];
      if (init === undefined || init === null) return;
      if (typeof init === "string") {
        this._parse(init);
      } else if (init instanceof URLSearchParams) {
        this[listKey] = init[listKey].map(function (pair) { return [pair[0], pair[1]]; });
      } else if (typeof init[Symbol.iterator] === "function") {
        for (const pair of init) {
          const items = Array.from(pair);
          if (items.length !== 2) throw new TypeError("URLSearchParams pairs must have two items");
          this[listKey].push([String(items[0]), String(items[1])]);
        }
      } else {
        for (const key of Object.keys(init)) this[listKey].push([key, String(init[key])]);
      }
    }
    _parse(text) {
      this[listKey] = [];
      const source = text.charAt(0) === "?" ? text.slice(1) : text;
      if (source === "") return;
      for (const part of source.split("&")) {
        if (part === "") continue;
        const index = part.indexOf("=");
        const name = index < 0 ? part : part.slice(0, index);
        const value = index < 0 ? "" : part.slice(index + 1);
        this[listKey].push([formDecode(name), formDecode(value)]);
      }
    }
    _changed() { if (this[ownerKey]) { const text = this.toString(); this[ownerKey]._search = text ? "?" + text : ""; } }
    append(name, value) { this[listKey].push([String(name), String(value)]); this._changed(); }
    delete(name) { name = String(name); this[listKey] = this[listKey].filter(function (pair) { return pair[0] !== name; }); this._changed(); }
    get(name) { name = String(name); const pair = this[listKey].find(function (item) { return item[0] === name; }); return pair ? pair[1] : null; }
    getAll(name) { name = String(name); return this[listKey].filter(function (pair) { return pair[0] === name; }).map(function (pair) { return pair[1]; }); }
    has(name) { name = String(name); return this[listKey].some(function (pair) { return pair[0] === name; }); }
    set(name, value) {
      name = String(name); value = String(value);
      const index = this[listKey].findIndex(function (pair) { return pair[0] === name; });
      if (index < 0) {
        this[listKey].push([name, value]);
      } else {
        this[listKey][index][1] = value;
        this[listKey] = this[listKey].filter(function (pair, position) { return position <= index || pair[0] !== name; });
      }
      this._changed();
    }
    sort() { this[listKey].sort(function (a, b) { return a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0; }); this._changed(); }
    forEach(callback, thisArg) { for (const pair of this[listKey]) callback.call(thisArg, pair[1], pair[0], this); }
    keys() { return this[listKey].map(function (pair) { return pair[0]; })[Symbol.iterator](); }
    values() { return this[listKey].map(function (pair) { return pair[1]; })[Symbol.iterator](); }
    entries() { return this[listKey].map(function (pair) { return [pair[0], pair[1]]; })[Symbol.iterator](); }
    [Symbol.iterator]() { return this.entries(); }
    get size() { return this[listKey].length; }
    toString() { return this[listKey].map(function (pair) { return formEncode(pair[0]) + "=" + formEncode(pair[1]); }).join("&"); }
  }
  define("URLSearchParams", URLSearchParams);

  class URL {
    constructor(url, base) {
      const parts = native.parseURL(String(url), base === undefined ? undefined : String(base));
      this._protocol = parts.protocol;
      this._username = parts.username;
      this._password = parts.password;
      this._hostname = parts.hostname;
      this._port = parts.port;
      this._pathname = parts.pathname;
      this._search = parts.search;
      this._hash = parts.hash;
      this._special = parts.special;
      const params = new URLSearchParams(this._search);
      params[ownerKey] = this;
      Object.defineProperty(this, "_params", { value: params, enumerable: false });
    }
    static canParse(url, base) { try { new URL(url, base); return true; } catch (error) { return false; } }
    get protocol() { return this._protocol; }
    set protocol(value) { value = String(value); this._protocol = value.endsWith(":") ? value : value + ":"; }
    get username() { return this._username; }
    set username(value) { this._username = String(value); }
    get password() { return this._password; }
    set password(value) { this._password = String(value); }
    get hostname() { return this._hostname; }
    set hostname(value) { this._hostname = String(value); }
    get port() { return this._port; }
    set port(value) { this._port = String(value); }
    get host() { return this._port ? this._hostname + ":" + this._port : this._hostname; }
    set host(value) { const parts = String(value).split(":"); this._hostname = parts[0]; this._port = parts[1] || ""; }
    get origin() { return this._special && this._hostname ? this._protocol + "//" + this.host : "null"; }
    get pathname() { return this._pathname; }
    set pathname(value) { value = String(value); this._pathname = value.charAt(0) === "/" || !this._hostname ? value : "/" + value; }
    get search() { return this._search === "?" ? "" : this._search; }
    set search(value) {
      value = String(value);
      this._search = value === "" ? "" : (value.charAt(0) === "?" ? value : "?" + value);
      this._params._parse(this._search);
    }
    get searchParams() { return this._params; }
    get hash() { return this._hash; }
    set hash(value) { value = String(value); this._hash = value === "" ? "" : (value.charAt(0) === "#" ? value : "#" + value); }
    get href() {
      let auth = "";
      if (this._username || this._password) {
        auth = this._username + (this._password ? ":" + this._password : "") + "@";
      }
      const authority = this._hostname || this._special ? "//" + auth + this.host : "";
      return this._protocol + authority + this._pathname + this.search + this._hash;
    }
    set href(value) {
      const next = new URL(value);
      for (const key of ["_protocol", "_username", "_password", "_hostname", "_port", "_pathname", "_search", "_hash", "_special"]) this[key] = next[key];
      this._params._parse(this._search);
    }
    toString() { return this.href; }
    toJSON() { return this.href; }
  }
  define("URL", URL);

  define("setTimeout", function setTimeout(callback) {
    const args = Array.prototype.slice.call(arguments);
    return native.setTimeout.apply(undefined, args);
  });
  define("clearTimeout", function clearTimeout(id) { if (id !== undefined && id !== null) native.clearTimeout(id); });

  function format(values) {
    return values.map(function (value) {
      if (typeof value === "string") return value;
      if (value instanceof Error) return value.stack || String(value);
      if (typeof value === "object" && value !== null) {
        try { const text = JSON.stringify(value); if (text !== undefined) return text; } catch (error) { /* fall through */ }
      }
      return String(value);
    }).join(" ");
  }
  function logger(level) {
    return function () { native.log(level, format(Array.prototype.slice.call(arguments))); };
  }
  define("console", Object.freeze({
    log: logger("info"), info: logger("info"), debug: logger("info"),
    warn: logger("warn"), error: logger("error"),
  }));
})(globalThis, globalThis.__autable_native);
`
