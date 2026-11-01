import {SummernoteWrapper} from "./forms/SummernoteWrapper";
import {t} from "./i18n";

export class Messages {
    public static initSummerNote() {
        let $contentManagerMessageContent = $("#ContentManagerMessageContent");

        if (!$contentManagerMessageContent.length) {
            return;
        }

        let $summerNote = $("#contentManagerWelcomeMessage");
        let html = "";

        if ($contentManagerMessageContent.length > 0) {
            html = $contentManagerMessageContent.html();
        }

        let wrapper = new SummernoteWrapper($summerNote, {
            placeholder: t('messages.cm_placeholder'),
            tabsize: 2,
            height: 400,
        }, html);

        wrapper.render();
    }
}
