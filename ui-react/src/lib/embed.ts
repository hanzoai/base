// Base is one admin with two places to be: its own page at base.hanzo.ai, and a
// section inside a Hanzo surface that offers Base as one of its products.
//
// Which one is not a setting. It is a fact the browser already holds — a
// document that is not its own top is in somebody else's page — so it is read
// rather than configured, and there is no build flag, query parameter or env
// var that can disagree with where the thing actually is.
//
// Comparing the two window references touches no property of either, so it is
// allowed across origins. A context that refuses even that is one we cannot see
// out of, which is the framed answer.
export const embedded: boolean = (() => {
  try {
    return window.self !== window.top
  } catch {
    return true
  }
})()
