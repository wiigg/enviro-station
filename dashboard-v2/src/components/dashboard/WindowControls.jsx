import { memo } from "react";

export default memo(function WindowControls({
  onSelectWindow,
  selectedWindowId,
  windowOptions
}) {
  return (
    <section className="controls" aria-label="Dashboard time range">
      <p className="controlLabel">Time range</p>
      <fieldset className="controlGroup">
        <legend className="visuallyHidden">Time range</legend>
        {windowOptions.map((windowOption) => (
          <button
            key={windowOption.id}
            className={`segmentButton ${windowOption.id === selectedWindowId ? "segmentActive" : ""}`}
            type="button"
            aria-pressed={windowOption.id === selectedWindowId}
            onClick={() => onSelectWindow(windowOption.id)}
          >
            {windowOption.label}
          </button>
        ))}
      </fieldset>
    </section>
  );
});
