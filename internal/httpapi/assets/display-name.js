(() => {
  "use strict";

  const form = document.querySelector("[data-display-name-form]");
  if (!form) return;

  const input = form.querySelector("#display-name");
  const feedback = form.querySelector("#display-name-validation");
  const submit = form.querySelector('button[type="submit"]');
  const randomNameButton = form.querySelector("[data-random-name]");
  const allowedSymbols = new Set(" _-.()[]{}!?@#$%^&*+=~'");
  const minLength = input.minLength;
  const maxLength = input.maxLength;
  const adjectives = [
    "Agile", "Amber", "Brave", "Bright", "Calm", "Clever", "Cosmic", "Daring",
    "Gentle", "Golden", "Happy", "Jolly", "Lucky", "Mighty", "Nimble", "Rapid",
    "Silver", "Solar", "Steady", "Swift", "Turbo", "Valiant", "Wild",
  ];
  const nouns = [
    "Badger", "Bear", "Bison", "Cobra", "Eagle", "Falcon", "Fox", "Gecko",
    "Hawk", "Heron", "Jaguar", "Koala", "Lynx", "Mantis", "Otter", "Owl",
    "Panda", "Raven", "Shark", "Tiger", "Toucan", "Wolf", "Yak",
  ];
  let touched = input.value.length > 0;

  const normalize = (value) => value.trim().replace(/\s+/g, " ");

  const validate = () => {
    const value = normalize(input.value);
    let message = "";

    if (value.length < minLength) {
      message = `Use at least ${minLength} characters.`;
    } else if (value.length > maxLength) {
      message = `Use no more than ${maxLength} characters.`;
    } else if (![...value].every((character) => {
      const code = character.charCodeAt(0);
      const asciiLetterOrNumber = code >= 48 && code <= 57
        || code >= 65 && code <= 90
        || code >= 97 && code <= 122;
      return asciiLetterOrNumber || allowedSymbols.has(character);
    })) {
      message = "That character is not supported.";
    }

    input.setCustomValidity(message);
    submit.disabled = message !== "";
    feedback.textContent = touched ? (message || "Looks good.") : "";
    feedback.classList.toggle("validation-valid", touched && message === "");
    feedback.classList.toggle("validation-invalid", touched && message !== "");
    return message === "";
  };

  input.addEventListener("input", () => {
    touched = true;
    validate();
  });

  input.addEventListener("blur", () => {
    input.value = normalize(input.value);
    validate();
  });

  randomNameButton.addEventListener("click", () => {
    const randomValues = new Uint32Array(3);
    window.crypto.getRandomValues(randomValues);
    const adjective = adjectives[randomValues[0] % adjectives.length];
    const noun = nouns[randomValues[1] % nouns.length];
    const number = 10 + (randomValues[2] % 90);
    input.value = `${adjective}${noun}${number}`;
    touched = true;
    validate();
    input.focus();
  });

  form.addEventListener("submit", (event) => {
    touched = true;
    input.value = normalize(input.value);
    if (!validate()) {
      event.preventDefault();
      input.reportValidity();
    }
  });

  validate();
})();
