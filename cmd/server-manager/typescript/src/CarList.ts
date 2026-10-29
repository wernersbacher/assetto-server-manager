export class CarList {
    public constructor() {
        // deleting a car/track from the list is confirmed by DeleteConfirm (data-confirm-delete on the form);
        // stop the click from bubbling into the clickable card.
        $(".delete-car").on("click", function(e) {
            e.stopPropagation();
        });
    }
}
