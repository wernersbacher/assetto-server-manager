// ToggleOverride links a checkbox to a field: <input type="checkbox" data-toggle-override="#field-id"> enables the
// field while it is checked and disables it otherwise (a disabled field is not submitted, so the server decides by
// the checkbox). The field shows the default value while disabled; the page works without this script because the
// checkbox alone decides what is stored.
export class ToggleOverride {
    public constructor() {
        $(document).on("change", "input[type=checkbox][data-toggle-override]", (e) => {
            const box = e.currentTarget as HTMLInputElement;

            $(box.dataset.toggleOverride as string).prop("disabled", !box.checked);
        });
    }
}
