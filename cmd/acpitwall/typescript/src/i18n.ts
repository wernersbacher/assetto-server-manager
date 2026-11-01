// Translations for the browser. layout/base.html sets window.I18N to the language of the page and the messages below
// "js." of the catalogs (cmd/acpitwall/views/locales), with the prefix removed: t("confirm.title") is the catalog key
// js.confirm.title. Placeholders look like {name} and are filled from params; plurals are sub keys named after the
// plural form (one, other, ...), see tn. Missing keys give the key itself, so that they stand out.
//
// Adding a language: besides its catalog files, import its moment locale below (and the FullCalendar locale in
// Calendar.ts), so that dates are formatted in it too.
import moment from "moment";
import "moment/locale/de";

interface I18NData {
    lang: string;
    messages: { [key: string]: string };
}

declare global {
    interface Window {
        I18N?: I18NData;
    }
}

const data: I18NData = (typeof window !== "undefined" && window.I18N) || {lang: "en", messages: {}};

moment.locale(data.lang);

export type Params = { [name: string]: string | number };

// lang returns the code of the language of the page, e.g. "de".
export function lang(): string {
    return data.lang;
}

function fill(message: string, params?: Params): string {
    if (!params) {
        return message;
    }

    return message.replace(/\{([A-Za-z0-9_]+)\}/g, (match, name) => {
        return params.hasOwnProperty(name) ? String(params[name]) : match;
    });
}

// t returns the message of the key (below js.) with its placeholders filled: t("live.lap", {lap: 3}).
export function t(key: string, params?: Params): string {
    const message = data.messages[key];

    if (message === undefined) {
        return key;
    }

    return fill(message, params);
}

// tn returns the plural form of the key which fits count, with {count} set: tn("live.laps", 3) uses js.live.laps.other.
export function tn(key: string, count: number, params?: Params): string {
    let form = count === 1 ? "one" : "other";

    try {
        form = new Intl.PluralRules(data.lang).select(count);
    } catch (e) {
        // old browsers: one/other
    }

    let message = data.messages[key + "." + form];

    if (message === undefined) {
        message = data.messages[key + ".other"];
    }

    if (message === undefined) {
        return key;
    }

    return fill(message, Object.assign({count: count}, params));
}
