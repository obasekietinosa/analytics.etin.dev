(function () {
  "use strict";

  if (typeof window === "undefined" || typeof document === "undefined") return;

  var scriptEl = document.currentScript;
  var endpointBase = "https://analytics.etin.dev";
  var siteId = "etin.dev";

  if (scriptEl) {
    if (scriptEl.src) {
      try {
        var parsedUrl = new URL(scriptEl.src);
        endpointBase = parsedUrl.origin;
      } catch (_e) {}
    }
    if (scriptEl.getAttribute("data-site-id")) {
      siteId = scriptEl.getAttribute("data-site-id") || siteId;
    }
    if (scriptEl.getAttribute("data-api-url")) {
      endpointBase = (scriptEl.getAttribute("data-api-url") || endpointBase).replace(/\/+$/, "");
    }
  }

  var CONSENT_KEY = "etin_analytics_consent";
  var COOKIE_NAME = "etin_vid";

  function uuid4() {
    if (typeof crypto !== "undefined" && crypto.randomUUID) {
      return crypto.randomUUID();
    }
    return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, function (c) {
      var r = (Math.random() * 16) | 0;
      var v = c === "x" ? r : (r & 0x3) | 0x8;
      return v.toString(16);
    });
  }

  function getCookie(name) {
    var match = document.cookie.match(new RegExp("(^| )" + name + "=([^;]+)"));
    return match ? decodeURIComponent(match[2]) : "";
  }

  function setCookie(name, value, days) {
    var maxAge = days * 24 * 60 * 60;
    var secure = window.location.protocol === "https:" ? "; Secure" : "";
    document.cookie =
      name + "=" + encodeURIComponent(value) + "; Path=/; Max-Age=" + maxAge + "; SameSite=Lax" + secure;
  }

  function deleteCookie(name) {
    document.cookie = name + "=; Path=/; Max-Age=0; SameSite=Lax";
  }

  function getConsentStatus() {
    try {
      return localStorage.getItem(CONSENT_KEY); // "granted" | "declined" | null
    } catch (_e) {
      return null;
    }
  }

  function getOrCreateVisitorId() {
    if (getConsentStatus() !== "granted") {
      return "";
    }
    var vid = getCookie(COOKIE_NAME);
    if (!vid) {
      vid = uuid4();
      setCookie(COOKIE_NAME, vid, 180);
    }
    return vid;
  }

  function sendPayload(path, payload) {
    var url = endpointBase + path;
    var body = JSON.stringify(payload);
    if (navigator.sendBeacon) {
      try {
        var blob = new Blob([body], { type: "application/json" });
        if (navigator.sendBeacon(url, blob)) return;
      } catch (_e) {}
    }
    fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: body,
      keepalive: true,
    }).catch(function () {});
  }

  var currentPageviewId = uuid4();
  var activeSeconds = 0;
  var maxScrollDepth = 0;
  var lastActivityAt = Date.now();
  var lastFlushedSeconds = 0;
  var lastFlushedScroll = 0;

  function computeScrollDepth() {
    var docEl = document.documentElement;
    var body = document.body;
    var scrollTop = window.scrollY || docEl.scrollTop || body.scrollTop || 0;
    var viewportHeight = window.innerHeight || docEl.clientHeight || 0;
    var fullHeight = Math.max(
      body.scrollHeight || 0,
      docEl.scrollHeight || 0,
      body.offsetHeight || 0,
      docEl.offsetHeight || 0
    );
    if (fullHeight <= viewportHeight || fullHeight === 0) return 100;
    var pct = Math.round(((scrollTop + viewportHeight) / fullHeight) * 100);
    var milestone = Math.floor(Math.min(100, Math.max(0, pct)) / 25) * 25;
    return milestone;
  }

  function flushEngagement() {
    if (activeSeconds <= lastFlushedSeconds && maxScrollDepth <= lastFlushedScroll) {
      return;
    }
    lastFlushedSeconds = activeSeconds;
    lastFlushedScroll = maxScrollDepth;
    sendPayload("/v1/engagement", {
      pageviewId: currentPageviewId,
      durationSeconds: activeSeconds,
      maxScrollDepth: maxScrollDepth,
    });
  }

  function trackPageview() {
    currentPageviewId = uuid4();
    activeSeconds = 0;
    maxScrollDepth = computeScrollDepth();
    lastFlushedSeconds = 0;
    lastFlushedScroll = 0;

    var params = new URLSearchParams(window.location.search);
    sendPayload("/v1/collect", {
      pageviewId: currentPageviewId,
      siteId: siteId,
      path: window.location.pathname,
      referrer: document.referrer || "",
      utmSource: params.get("utm_source") || "",
      utmMedium: params.get("utm_medium") || "",
      utmCampaign: params.get("utm_campaign") || "",
      visitorId: getOrCreateVisitorId(),
    });
  }

  // Track scroll milestones (25%, 50%, 75%, 100%)
  window.addEventListener(
    "scroll",
    function () {
      lastActivityAt = Date.now();
      var milestone = computeScrollDepth();
      if (milestone > maxScrollDepth) {
        maxScrollDepth = milestone;
      }
    },
    { passive: true }
  );

  ["mousemove", "keydown", "touchstart", "click"].forEach(function (evt) {
    window.addEventListener(
      evt,
      function () {
        lastActivityAt = Date.now();
      },
      { passive: true }
    );
  });

  // Heartbeat timer: counts active reading seconds only while tab is visible and recently active
  setInterval(function () {
    if (document.visibilityState === "visible" && Date.now() - lastActivityAt < 60000) {
      activeSeconds += 1;
      if (activeSeconds % 15 === 0) {
        flushEngagement();
      }
    }
  }, 1000);

  document.addEventListener("visibilitychange", function () {
    if (document.visibilityState === "hidden") {
      flushEngagement();
    }
  });

  window.addEventListener("pagehide", flushEngagement);

  // Track outbound link clicks automatically
  document.addEventListener("click", function (event) {
    var target = event.target;
    while (target && target !== document && target.tagName !== "A") {
      target = target.parentNode;
    }
    if (!target || !target.href) return;
    try {
      var linkUrl = new URL(target.href, window.location.origin);
      if (
        (linkUrl.protocol === "http:" || linkUrl.protocol === "https:") &&
        linkUrl.hostname !== window.location.hostname &&
        !linkUrl.hostname.endsWith(".etin.dev") &&
        linkUrl.hostname !== "etin.dev"
      ) {
        sendPayload("/v1/events", {
          siteId: siteId,
          pageviewId: currentPageviewId,
          eventName: "outbound_click",
          path: window.location.pathname,
          targetUrl: linkUrl.href,
        });
      }
    } catch (_e) {}
  });

  // Public API for GDPR Cookie Consent Banner & Custom Events
  window.etinAnalytics = {
    getConsent: getConsentStatus,
    optIn: function () {
      try {
        localStorage.setItem(CONSENT_KEY, "granted");
      } catch (_e) {}
      getOrCreateVisitorId();
      trackPageview();
    },
    optOut: function () {
      try {
        localStorage.setItem(CONSENT_KEY, "declined");
      } catch (_e) {}
      deleteCookie(COOKIE_NAME);
    },
    track: function (eventName, metadata) {
      if (!eventName) return;
      sendPayload("/v1/events", {
        siteId: siteId,
        pageviewId: currentPageviewId,
        eventName: String(eventName),
        path: window.location.pathname,
        metadata: metadata || {},
      });
    },
  };

  // Initial pageview
  trackPageview();
})();
