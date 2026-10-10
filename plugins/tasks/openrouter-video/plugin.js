// The catalog is refreshed outside synchronous hooks by update-catalog.py.
// BEGIN VIDEO CATALOG
const catalog = {
  "fetched_at": "2026-10-09T20:02:55.839033+00:00",
  "models": [
    {
      "id": "x-ai/grok-imagine-video-1.5-lite",
      "canonical_slug": "x-ai/grok-imagine-video-1.5-lite-20261001",
      "supported_durations": [
        1,
        2,
        3,
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "480p",
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16",
        "1:1",
        "4:3",
        "3:4",
        "3:2",
        "2:3"
      ],
      "supported_sizes": null,
      "supported_frame_images": [
        "first_frame"
      ],
      "generate_audio": null,
      "seed": null,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [],
      "pricing_skus": {
        "cents_per_image_input": "1",
        "cents_per_video_output_second_480p": "2",
        "cents_per_video_output_second_720p": "3",
        "cents_per_video_output_second_1080p": "14"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "heygen/heygen-video-1",
      "canonical_slug": "heygen/heygen-video-1-20260930",
      "supported_durations": [
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "480p",
        "768p",
        "2K"
      ],
      "supported_aspect_ratios": [
        "21:9",
        "16:9",
        "4:3",
        "1:1",
        "3:4",
        "9:16"
      ],
      "supported_sizes": null,
      "supported_frame_images": [
        "first_frame"
      ],
      "generate_audio": false,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "prompt_enhancement"
      ],
      "pricing_skus": {
        "duration_seconds_2k": "0.09",
        "duration_seconds_480p": "0.02",
        "duration_seconds_768p": "0.03",
        "reference_duration_seconds_2k": "0.18",
        "reference_duration_seconds_480p": "0.04",
        "reference_duration_seconds_768p": "0.06"
      },
      "input_modalities": [
        "text",
        "image",
        "video",
        "audio"
      ]
    },
    {
      "id": "black-forest-labs/flux-video-edit",
      "canonical_slug": "black-forest-labs/flux-video-edit-20260910",
      "supported_durations": null,
      "supported_resolutions": null,
      "supported_aspect_ratios": null,
      "supported_sizes": null,
      "supported_frame_images": null,
      "generate_audio": false,
      "seed": false,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "safety_tolerance"
      ],
      "pricing_skus": {
        "cents_per_second_output": "3"
      },
      "input_modalities": [
        "text",
        "video"
      ]
    },
    {
      "id": "minimax/hailuo-3-max",
      "canonical_slug": "minimax/hailuo-3-max-20260901",
      "supported_durations": [
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "768p",
        "480p"
      ],
      "supported_aspect_ratios": [
        "21:9",
        "16:9",
        "4:3",
        "1:1",
        "3:4",
        "9:16"
      ],
      "supported_sizes": null,
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": false,
      "seed": false,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "aigc_watermark"
      ],
      "pricing_skus": {
        "duration_seconds": "0.08",
        "duration_seconds_480p": "0.05",
        "duration_seconds_768p": "0.08"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "alibaba/wan-3.0-prime",
      "canonical_slug": "alibaba/wan-3.0-prime-20260827",
      "supported_durations": [
        2,
        3,
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15,
        16,
        17,
        18,
        19,
        20,
        21,
        22,
        23,
        24,
        25,
        26,
        27,
        28,
        29,
        30
      ],
      "supported_resolutions": [
        "480p",
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "4:3",
        "1:1",
        "3:4",
        "9:16"
      ],
      "supported_sizes": null,
      "supported_frame_images": [
        "first_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [],
      "pricing_skus": {
        "duration_seconds_480p": "0.068",
        "duration_seconds_720p": "0.14",
        "duration_seconds_1080p": "0.28"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "alibaba/wan-3.0",
      "canonical_slug": "alibaba/wan-3.0-20260824",
      "supported_durations": [
        2,
        3,
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15,
        16,
        17,
        18,
        19,
        20,
        21,
        22,
        23,
        24,
        25,
        26,
        27,
        28,
        29,
        30
      ],
      "supported_resolutions": [
        "480p",
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "4:3",
        "1:1",
        "3:4",
        "9:16"
      ],
      "supported_sizes": null,
      "supported_frame_images": [
        "first_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [],
      "pricing_skus": {
        "duration_seconds_480p": "0.05",
        "duration_seconds_720p": "0.1",
        "duration_seconds_1080p": "0.2"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "heygen/avatar-iv",
      "canonical_slug": "heygen/avatar-iv-20260625",
      "supported_durations": null,
      "supported_resolutions": [
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16",
        "1:1"
      ],
      "supported_sizes": null,
      "supported_frame_images": null,
      "generate_audio": false,
      "seed": false,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "voice_id",
        "voice_settings",
        "motion_prompt",
        "expressiveness",
        "fit",
        "remove_background",
        "background",
        "caption",
        "title"
      ],
      "pricing_skus": {
        "duration_seconds": "0.05"
      },
      "input_modalities": [
        "text",
        "image",
        "audio"
      ]
    },
    {
      "id": "black-forest-labs/flux-video-upscale",
      "canonical_slug": "black-forest-labs/flux-video-upscale-20260819",
      "supported_durations": null,
      "supported_resolutions": null,
      "supported_aspect_ratios": null,
      "supported_sizes": null,
      "supported_frame_images": null,
      "generate_audio": false,
      "seed": false,
      "upscale_factor": {
        "min": 1.5,
        "max": 3
      },
      "creativity": [
        0,
        1
      ],
      "allowed_passthrough_parameters": [
        "safety_tolerance"
      ],
      "pricing_skus": {
        "cents_per_megapixel_second_precise": "7.5",
        "cents_per_megapixel_second_creative": "10.5"
      },
      "input_modalities": [
        "text",
        "video"
      ]
    },
    {
      "id": "bytedance/seedance-2.0-mini",
      "canonical_slug": "bytedance/seedance-2.0-mini-20260811",
      "supported_durations": [
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "480p",
        "720p"
      ],
      "supported_aspect_ratios": [
        "1:1",
        "3:4",
        "9:16",
        "4:3",
        "16:9",
        "21:9",
        "9:21"
      ],
      "supported_sizes": [
        "480x480",
        "480x640",
        "480x854",
        "640x480",
        "854x480",
        "1120x480",
        "720x720",
        "720x960",
        "720x1280",
        "720x1680",
        "960x720",
        "1280x720",
        "1680x720"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "watermark",
        "req_key",
        "return_last_frame"
      ],
      "pricing_skus": {
        "video_tokens": "0.0000035",
        "video_tokens_without_audio": "0.0000035",
        "video_tokens_with_video_input": "0.0000021"
      },
      "input_modalities": [
        "text",
        "image",
        "video",
        "audio"
      ]
    },
    {
      "id": "bytedance/seedance-2.5",
      "canonical_slug": "bytedance/seedance-2.5-20260807",
      "supported_durations": [
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15,
        16,
        17,
        18,
        19,
        20,
        21,
        22,
        23,
        24,
        25,
        26,
        27,
        28,
        29,
        30
      ],
      "supported_resolutions": [
        "480p",
        "720p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "4:3",
        "1:1",
        "3:4",
        "9:16",
        "21:9"
      ],
      "supported_sizes": [
        "854x480",
        "752x560",
        "640x640",
        "560x752",
        "480x854",
        "992x432",
        "1280x720",
        "1112x834",
        "960x960",
        "834x1112",
        "720x1280",
        "1470x630"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "watermark",
        "req_key",
        "output_format"
      ],
      "pricing_skus": {
        "video_tokens": "0.0000107",
        "video_tokens_without_audio": "0.0000107",
        "video_tokens_with_video_input": "0.0000064"
      },
      "input_modalities": [
        "text",
        "image",
        "video",
        "audio"
      ]
    },
    {
      "id": "black-forest-labs/flux-3-video",
      "canonical_slug": "black-forest-labs/flux-3-video-20260804",
      "supported_durations": [
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15,
        16,
        17,
        18,
        19,
        20
      ],
      "supported_resolutions": [
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "21:9",
        "16:9",
        "4:3",
        "1:1",
        "3:4",
        "9:16"
      ],
      "supported_sizes": null,
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": false,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "safety_tolerance",
        "version"
      ],
      "pricing_skus": {
        "cents_per_second_output": "17",
        "cents_per_second_output_720p": "17",
        "cents_per_second_output_1080p": "29",
        "cents_per_second_video_continuation_720p": "41",
        "cents_per_second_video_continuation_1080p": "53"
      },
      "input_modalities": [
        "text",
        "image",
        "video"
      ]
    },
    {
      "id": "minimax/hailuo-3",
      "canonical_slug": "minimax/hailuo-03-20260730",
      "supported_durations": [
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "2K"
      ],
      "supported_aspect_ratios": [
        "21:9",
        "16:9",
        "4:3",
        "1:1",
        "3:4",
        "9:16"
      ],
      "supported_sizes": null,
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": false,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "aigc_watermark"
      ],
      "pricing_skus": {
        "duration_seconds": "0.13",
        "reference_images": "0.04"
      },
      "input_modalities": [
        "text",
        "image",
        "video",
        "audio"
      ]
    },
    {
      "id": "runway/aleph-2",
      "canonical_slug": "runway/aleph-2-20260729",
      "supported_durations": null,
      "supported_resolutions": null,
      "supported_aspect_ratios": [
        "16:9",
        "4:3",
        "3:2",
        "1:1",
        "2:3",
        "3:4",
        "9:16",
        "21:9"
      ],
      "supported_sizes": null,
      "supported_frame_images": null,
      "generate_audio": false,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "contentModeration",
        "keyframes"
      ],
      "pricing_skus": {
        "cents_per_second_output": "28",
        "minimum_cents_per_generation": "56"
      },
      "input_modalities": [
        "text",
        "image",
        "video"
      ]
    },
    {
      "id": "runway/gen-4.5",
      "canonical_slug": "runway/gen-4.5-20260729",
      "supported_durations": [
        2,
        3,
        4,
        5,
        6,
        7,
        8,
        9,
        10
      ],
      "supported_resolutions": [
        "720p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16"
      ],
      "supported_sizes": [
        "1280x720",
        "720x1280"
      ],
      "supported_frame_images": [
        "first_frame"
      ],
      "generate_audio": false,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "contentModeration"
      ],
      "pricing_skus": {
        "cents_per_second_output": "12"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "x-ai/grok-imagine-video-1.5",
      "canonical_slug": "x-ai/grok-imagine-video-1.5-20260719",
      "supported_durations": [
        1,
        2,
        3,
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "480p",
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16",
        "1:1",
        "4:3",
        "3:4",
        "3:2",
        "2:3"
      ],
      "supported_sizes": null,
      "supported_frame_images": [
        "first_frame"
      ],
      "generate_audio": null,
      "seed": null,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [],
      "pricing_skus": {
        "cents_per_image_input": "1",
        "cents_per_video_output_second_480p": "8",
        "cents_per_video_output_second_720p": "14",
        "cents_per_video_output_second_1080p": "25"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "alibaba/happyhorse-1.1",
      "canonical_slug": "alibaba/happyhorse-1.1-20260624",
      "supported_durations": [
        3,
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16",
        "1:1",
        "4:3",
        "3:4",
        "21:9",
        "9:21"
      ],
      "supported_sizes": [
        "1280x720",
        "720x1280",
        "720x720",
        "960x720",
        "720x960",
        "1680x720",
        "720x1680",
        "1920x1080",
        "1080x1920",
        "1080x1080",
        "1440x1080",
        "1080x1440",
        "2520x1080",
        "1080x2520"
      ],
      "supported_frame_images": [
        "first_frame"
      ],
      "generate_audio": null,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [],
      "pricing_skus": {
        "duration_seconds_720p": "0.0988",
        "duration_seconds_1080p": "0.1278"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "alibaba/happyhorse-1.0",
      "canonical_slug": "alibaba/happyhorse-1.0-20260624",
      "supported_durations": [
        3,
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16",
        "1:1",
        "4:3",
        "3:4",
        "21:9",
        "9:21"
      ],
      "supported_sizes": [
        "1280x720",
        "720x1280",
        "720x720",
        "960x720",
        "720x960",
        "1680x720",
        "720x1680",
        "1920x1080",
        "1080x1920",
        "1080x1080",
        "1440x1080",
        "1080x1440",
        "2520x1080",
        "1080x2520"
      ],
      "supported_frame_images": [
        "first_frame"
      ],
      "generate_audio": null,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [],
      "pricing_skus": {
        "duration_seconds_720p": "0.0988",
        "duration_seconds_1080p": "0.1694"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "x-ai/grok-imagine-video",
      "canonical_slug": "x-ai/grok-imagine-video-20260512",
      "supported_durations": [
        1,
        2,
        3,
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "480p",
        "720p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16",
        "1:1",
        "4:3",
        "3:4",
        "3:2",
        "2:3"
      ],
      "supported_sizes": [
        "854x480",
        "1280x720",
        "480x854",
        "720x1280",
        "480x480",
        "720x720",
        "640x480",
        "960x720",
        "480x640",
        "720x960",
        "720x480",
        "1080x720",
        "480x720",
        "720x1080"
      ],
      "supported_frame_images": [
        "first_frame"
      ],
      "generate_audio": null,
      "seed": null,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [],
      "pricing_skus": {
        "cents_per_image_input": "0.2",
        "cents_per_video_output_second_480p": "5",
        "cents_per_video_output_second_720p": "7"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "kwaivgi/kling-v3.0-pro",
      "canonical_slug": "kwaivgi/kling-v3.0-pro-20260429",
      "supported_durations": [
        3,
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "720p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16",
        "1:1"
      ],
      "supported_sizes": [
        "1280x720",
        "720x1280",
        "720x720"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": false,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "negative_prompt",
        "cfg_scale"
      ],
      "pricing_skus": {
        "duration_seconds": "0.112",
        "duration_seconds_with_audio": "0.168",
        "text_to_video_duration_seconds_480p": "0.112",
        "text_to_video_duration_seconds_720p": "0.112",
        "image_to_video_duration_seconds_720p": "0.112",
        "text_to_video_duration_seconds_1080p": "0.112",
        "image_to_video_duration_seconds_1080p": "0.112"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "kwaivgi/kling-v3.0-std",
      "canonical_slug": "kwaivgi/kling-v3.0-std-20260429",
      "supported_durations": [
        3,
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "720p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16",
        "1:1"
      ],
      "supported_sizes": [
        "1280x720",
        "720x1280",
        "720x720"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": false,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "negative_prompt",
        "cfg_scale"
      ],
      "pricing_skus": {
        "duration_seconds": "0.084",
        "duration_seconds_with_audio": "0.126",
        "text_to_video_duration_seconds_480p": "0.084",
        "text_to_video_duration_seconds_720p": "0.084",
        "image_to_video_duration_seconds_720p": "0.084",
        "text_to_video_duration_seconds_1080p": "0.084",
        "image_to_video_duration_seconds_1080p": "0.084"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "google/veo-3.1-fast",
      "canonical_slug": "google/veo-3.1-fast-20260320",
      "supported_durations": [
        4,
        6,
        8
      ],
      "supported_resolutions": [
        "720p",
        "1080p",
        "4K"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16"
      ],
      "supported_sizes": [
        "1280x720",
        "1080x1920",
        "1920x1080",
        "720x1280",
        "3840x2160",
        "2160x3840"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "personGeneration",
        "aspectRatio",
        "negativePrompt",
        "conditioningScale",
        "enhancePrompt"
      ],
      "pricing_skus": {
        "duration_seconds_with_audio": "0.12",
        "duration_seconds_with_audio_4k": "0.30",
        "duration_seconds_without_audio": "0.10",
        "duration_seconds_with_audio_720p": "0.10",
        "duration_seconds_without_audio_4k": "0.25",
        "duration_seconds_without_audio_720p": "0.08"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "google/veo-3.1-lite",
      "canonical_slug": "google/veo-3.1-lite-20260331",
      "supported_durations": [
        8,
        4,
        6
      ],
      "supported_resolutions": [
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16"
      ],
      "supported_sizes": [
        "1280x720",
        "720x1280",
        "1920x1080",
        "1080x1920"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "personGeneration",
        "aspectRatio",
        "negativePrompt",
        "conditioningScale",
        "enhancePrompt"
      ],
      "pricing_skus": {
        "duration_seconds_with_audio": "0.08",
        "duration_seconds_without_audio": "0.05",
        "duration_seconds_with_audio_720p": "0.05",
        "duration_seconds_without_audio_720p": "0.03"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "kwaivgi/kling-video-o1",
      "canonical_slug": "kwaivgi/kling-video-o1-20260420",
      "supported_durations": [
        5,
        10
      ],
      "supported_resolutions": [
        "720p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16",
        "1:1"
      ],
      "supported_sizes": [
        "1280x720",
        "720x1280",
        "720x720"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": false,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "negative_prompt"
      ],
      "pricing_skus": {
        "duration_seconds": "0.1120"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "minimax/hailuo-2.3",
      "canonical_slug": "minimax/hailuo-2.3-20260420",
      "supported_durations": [
        6,
        10
      ],
      "supported_resolutions": [
        "1080p"
      ],
      "supported_aspect_ratios": [
        "16:9"
      ],
      "supported_sizes": [
        "1920x1080"
      ],
      "supported_frame_images": [
        "first_frame"
      ],
      "generate_audio": false,
      "seed": null,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "prompt_optimizer",
        "fast_pretreatment"
      ],
      "pricing_skus": {
        "duration_seconds": "0.0817"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "bytedance/seedance-2.0",
      "canonical_slug": "bytedance/seedance-2.0-20260414",
      "supported_durations": [
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "480p",
        "720p",
        "1080p",
        "4K"
      ],
      "supported_aspect_ratios": [
        "1:1",
        "3:4",
        "9:16",
        "4:3",
        "16:9",
        "21:9",
        "9:21"
      ],
      "supported_sizes": [
        "480x480",
        "480x640",
        "480x854",
        "640x480",
        "854x480",
        "1120x480",
        "720x720",
        "720x960",
        "720x1280",
        "720x1680",
        "960x720",
        "1280x720",
        "1680x720",
        "1080x1080",
        "1080x1440",
        "1080x1920",
        "1440x1080",
        "1920x1080",
        "2520x1080",
        "3840x2160",
        "2160x3840",
        "2160x2160",
        "2880x2160",
        "2160x2880",
        "5040x2160"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "watermark",
        "req_key"
      ],
      "pricing_skus": {
        "video_tokens": "0.000007",
        "video_tokens_4k": "0.000004",
        "video_tokens_1080p": "0.0000077",
        "video_tokens_without_audio": "0.000007",
        "video_tokens_with_video_input": "0.0000043",
        "video_tokens_4k_with_video_input": "0.0000024",
        "video_tokens_1080p_with_video_input": "0.0000047"
      },
      "input_modalities": [
        "text",
        "image",
        "video",
        "audio"
      ]
    },
    {
      "id": "bytedance/seedance-2.0-fast",
      "canonical_slug": "bytedance/seedance-2.0-fast-20260414",
      "supported_durations": [
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12,
        13,
        14,
        15
      ],
      "supported_resolutions": [
        "480p",
        "720p"
      ],
      "supported_aspect_ratios": [
        "1:1",
        "3:4",
        "9:16",
        "4:3",
        "16:9",
        "21:9",
        "9:21"
      ],
      "supported_sizes": [
        "480x480",
        "480x640",
        "480x854",
        "640x480",
        "854x480",
        "1120x480",
        "720x720",
        "720x960",
        "720x1280",
        "720x1680",
        "960x720",
        "1280x720",
        "1680x720"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "watermark",
        "req_key"
      ],
      "pricing_skus": {
        "video_tokens": "0.0000042",
        "video_tokens_without_audio": "0.0000042",
        "video_tokens_with_video_input": "0.000002475"
      },
      "input_modalities": [
        "text",
        "image",
        "video",
        "audio"
      ]
    },
    {
      "id": "alibaba/wan-2.7",
      "canonical_slug": "alibaba/wan-2.7-20260414",
      "supported_durations": [
        2,
        3,
        4,
        5,
        6,
        7,
        8,
        9,
        10
      ],
      "supported_resolutions": [
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16",
        "1:1",
        "4:3",
        "3:4"
      ],
      "supported_sizes": [
        "1280x720",
        "720x1280",
        "1920x1080",
        "1080x1920",
        "720x720",
        "1080x1080",
        "960x720",
        "720x960",
        "1440x1080",
        "1080x1440"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "negative_prompt",
        "prompt_extend",
        "audio",
        "ratio",
        "last_image",
        "video",
        "videos",
        "images"
      ],
      "pricing_skus": {
        "duration_seconds": "0.1"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "alibaba/wan-2.6",
      "canonical_slug": "alibaba/wan-2.6-20260327",
      "supported_durations": [
        5,
        10
      ],
      "supported_resolutions": [
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16"
      ],
      "supported_sizes": [
        "1280x720",
        "1080x1920",
        "720x1280",
        "1920x1080"
      ],
      "supported_frame_images": [
        "first_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "negative_prompt",
        "enable_prompt_expansion",
        "shot_type",
        "audio",
        "size"
      ],
      "pricing_skus": {
        "text_to_video_duration_seconds_480p": "0.04",
        "text_to_video_duration_seconds_720p": "0.08",
        "image_to_video_duration_seconds_720p": "0.10",
        "text_to_video_duration_seconds_1080p": "0.12",
        "image_to_video_duration_seconds_1080p": "0.15"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "bytedance/seedance-1-5-pro",
      "canonical_slug": "bytedance/seedance-1-5-pro-20260320",
      "supported_durations": [
        4,
        5,
        6,
        7,
        8,
        9,
        10,
        11,
        12
      ],
      "supported_resolutions": [
        "480p",
        "720p",
        "1080p"
      ],
      "supported_aspect_ratios": [
        "1:1",
        "3:4",
        "9:16",
        "9:21",
        "4:3",
        "16:9",
        "21:9"
      ],
      "supported_sizes": [
        "480x480",
        "480x640",
        "480x854",
        "480x1120",
        "640x480",
        "720x720",
        "720x960",
        "720x1280",
        "720x1680",
        "854x480",
        "960x720",
        "1080x1080",
        "1080x1440",
        "1080x1920",
        "1080x2520",
        "1120x480",
        "1280x720",
        "1440x1080",
        "1680x720",
        "1920x1080",
        "2520x1080"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "watermark",
        "req_key"
      ],
      "pricing_skus": {
        "video_tokens": "0.0000024",
        "video_tokens_without_audio": "0.0000012"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    },
    {
      "id": "google/veo-3.1",
      "canonical_slug": "google/veo-3.1-20260320",
      "supported_durations": [
        4,
        6,
        8
      ],
      "supported_resolutions": [
        "720p",
        "1080p",
        "4K"
      ],
      "supported_aspect_ratios": [
        "16:9",
        "9:16"
      ],
      "supported_sizes": [
        "1280x720",
        "1080x1920",
        "1920x1080",
        "720x1280",
        "3840x2160",
        "2160x3840"
      ],
      "supported_frame_images": [
        "first_frame",
        "last_frame"
      ],
      "generate_audio": true,
      "seed": true,
      "upscale_factor": null,
      "creativity": null,
      "allowed_passthrough_parameters": [
        "personGeneration",
        "aspectRatio",
        "negativePrompt",
        "conditioningScale",
        "enhancePrompt"
      ],
      "pricing_skus": {
        "duration_seconds_with_audio": "0.40",
        "duration_seconds_with_audio_4k": "0.60",
        "duration_seconds_without_audio": "0.20",
        "duration_seconds_without_audio_4k": "0.40"
      },
      "input_modalities": [
        "text",
        "image"
      ]
    }
  ]
};
// END VIDEO CATALOG

export const meta = {
  apiVersion: 1,
  key: "openrouter-video",
  name: "OpenRouter Video",
  icon: "OpenRouter",
  version: "1.0.0",
  author: { name: "GetAPI" },
  description: { en: "Video generation through OpenRouter", zh: "通过 OpenRouter 生成视频" },
  baseUrl: "https://openrouter.ai/api",
  channelTypes: [20],
  models: catalog.models.map(function (model) { return model.id; }),
  fetchMode: "per_task",
  protocols: ["openai_video"],
  usageSchema: {
    requested_seconds: { type: "number", unit: "second", description: { en: "Requested video generation unit price", zh: "请求视频生成单价" } },
    resolution: { enum: ["default", "360p", "480p", "720p", "768p", "1080p", "1K", "2K", "4K"], description: { en: "Requested output video resolution", zh: "请求输出视频分辨率" } },
    size: { enum: ["default"].concat(Array.from(new Set(catalog.models.flatMap(function (model) { return model.supported_sizes || []; })))), description: { en: "Requested output video dimensions", zh: "请求输出视频尺寸" } },
    audio: { enum: ["default", "enabled", "disabled"], description: { en: "Requested audio generation", zh: "请求音频生成" } },
    images: { type: "number", unit: "count", description: { en: "Input image unit price", zh: "输入图像单价" } },
    videos: { type: "number", unit: "count", description: { en: "Input video unit price", zh: "输入视频单价" } },
    audios: { type: "number", unit: "count", description: { en: "Input audio unit price", zh: "输入音频单价" } },
    upscale_factor: { type: "number", unit: "count", description: { en: "Requested upscale factor; zero means omitted", zh: "请求放大倍数；零表示未指定" } },
    creativity: { enum: ["default", "0", "1"], description: { en: "Requested upscaling creativity", zh: "请求放大创造性" } },
    aspect_ratio: { enum: ["default"].concat(Array.from(new Set(catalog.models.flatMap(function (model) { return model.supported_aspect_ratios || []; })))), description: { en: "Requested output aspect ratio", zh: "请求输出宽高比" } },
    jobs: { type: "number", unit: "count", description: { en: "Video generation unit price", zh: "视频生成单价" } },
  },
};

function object(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

// This is shared by submission and reservation: facts always describe the
// exact outgoing order, never measured duration or inferred provider tokens.
function videoOrder(ctx) {
  const model = ctx.upstreamModel || ctx.model;
  const capability = catalog.models.find(function (item) { return item.id === model; });
  if (!capability) throw new Error("Unknown OpenRouter video model; refresh the video catalog");
  const age = utils.unixNow() * 1000 - Date.parse(catalog.fetched_at);
  if (!Number.isFinite(age) || age < -86400000 || age > 30 * 86400000)
    throw new Error("OpenRouter video catalog is stale; refresh it before submitting new jobs");
  const request = ctx.requestBody || {};
  if (!object(request)) throw new Error("Request body must be an object");
  if (request.metadata !== undefined && !object(request.metadata)) throw new Error("metadata must be an object");
  const parameters = Object.assign({}, request.metadata || {});
  for (const key of ["duration", "size", "resolution", "aspect_ratio", "generate_audio", "seed", "frame_images", "input_references", "provider", "upscale_factor", "creativity"]) {
    if (request[key] !== undefined) {
      if (parameters[key] !== undefined && JSON.stringify(parameters[key]) !== JSON.stringify(request[key]))
        throw new Error("Conflicting " + key + " values");
      parameters[key] = request[key];
    }
  }
  if (request.seconds !== undefined) {
    if (parameters.duration !== undefined && Number(parameters.duration) !== Number(request.seconds)) throw new Error("Conflicting seconds and duration values");
    parameters.duration = request.seconds;
  }
  const body = { model: model };
  if (request.prompt !== undefined) {
    if (typeof request.prompt !== "string" || !request.prompt.trim()) throw new Error("prompt must be a nonempty string");
    body.prompt = request.prompt;
  }
  if (parameters.duration !== undefined) {
    const seconds = Number(parameters.duration);
    if (!Number.isInteger(seconds) || seconds < 1 || seconds > 3600) throw new Error("seconds must be an integer between 1 and 3600");
    if (!(capability.supported_durations || []).includes(seconds)) throw new Error("Unsupported duration for " + model);
    body.duration = seconds;
  }
  for (const pair of [["resolution", "supported_resolutions"], ["aspect_ratio", "supported_aspect_ratios"], ["size", "supported_sizes"]]) {
    if (parameters[pair[0]] === undefined) continue;
    if (typeof parameters[pair[0]] !== "string" || !(capability[pair[1]] || []).includes(parameters[pair[0]]))
      throw new Error("Unsupported " + pair[0] + " for " + model);
    body[pair[0]] = parameters[pair[0]];
  }
  // OpenRouter treats size as an alternative to resolution/aspect_ratio.
  // Do not send two competing representations, even if individually valid.
  if (body.size !== undefined && (body.resolution !== undefined || body.aspect_ratio !== undefined))
    throw new Error("Use size or resolution/aspect_ratio, not both");
  for (const key of ["generate_audio", "seed", "upscale_factor", "creativity"]) {
    if (parameters[key] === undefined) continue;
    if (!capability[key]) throw new Error(key + " is not supported by " + model);
    const value = parameters[key];
    if (key === "generate_audio") {
      if (typeof value !== "boolean") throw new Error("generate_audio must be a boolean");
    } else if (key === "upscale_factor") {
      if (typeof value !== "number" || !Number.isFinite(value) || value < capability.upscale_factor.min || value > capability.upscale_factor.max)
        throw new Error("upscale_factor is outside the model's supported range");
    } else if (key === "creativity") {
      if (!capability.creativity.includes(value)) throw new Error("Unsupported creativity for " + model);
    } else if (!Number.isSafeInteger(value) || value < 0) throw new Error(key + " must be a nonnegative safe integer");
    body[key] = value;
  }
  const frames = parameters.frame_images === undefined ? [] : parameters.frame_images;
  if (!Array.isArray(frames)) throw new Error("frame_images must be an array");
  const references = parameters.input_references === undefined ? [] : parameters.input_references;
  if (!Array.isArray(references)) throw new Error("input_references must be an array");
  const images = [];
  if (request.images !== undefined && !Array.isArray(request.images)) throw new Error("images must be an array");
  for (const image of [request.input_reference, request.image].concat(request.images || [])) {
    if (image !== undefined && image !== null && image !== "") images.push(image);
  }
  if ((ctx.files || []).length) {
    if (ctx.files.length !== 1 || ctx.files[0].field !== "input_reference") throw new Error("Only one input_reference file is supported");
    images.push({ __fileRef: ctx.files[0].ref, encoding: "dataUrl", mimeType: ctx.files[0].mimeType, maxBytes: 20971520 });
  }
  if (images.length > 1 || (images.length && frames.length)) throw new Error("Conflicting input images; use frame_images for first and last frames");
  const normalizedFrames = frames.slice();
  if (images.length) normalizedFrames.push({ type: "image_url", image_url: { url: images[0] }, frame_type: "first_frame" });
  if (normalizedFrames.length > 2 || references.length > 128) throw new Error("Too many input references");
  const frameTypes = [];
  for (const frame of normalizedFrames) {
    if (!object(frame) || !(capability.supported_frame_images || []).includes(frame.frame_type) || frameTypes.includes(frame.frame_type))
      throw new Error("Unsupported or duplicate frame type for " + model);
    frameTypes.push(frame.frame_type);
    validateReference(frame, true);
  }
  let imageCount = normalizedFrames.length;
  let videoCount = 0;
  let audioCount = 0;
  for (const reference of references) {
    validateReference(reference, false);
    if (reference.type === "image_url") imageCount++;
    if (!(capability.input_modalities || []).includes(reference.type.replace("_url", "")))
      throw new Error("Unsupported input reference modality for " + model);
    if (reference.type === "video_url") videoCount++;
    if (reference.type === "audio_url") audioCount++;
  }
  if (normalizedFrames.length && references.length) throw new Error("Use frame_images or input_references, not both");
  if (imageCount > 128) throw new Error("Too many input images");
  if (!body.prompt && !normalizedFrames.length && !references.length) throw new Error("Provide a prompt or reference input");
  if (normalizedFrames.length) body.frame_images = normalizedFrames;
  if (references.length) body.input_references = references;
  if (parameters.provider !== undefined) {
    if (!object(parameters.provider) || Object.keys(parameters.provider).some(function (key) { return key !== "options"; }) || !object(parameters.provider.options))
      throw new Error("provider must contain an options object");
    for (const provider of Object.keys(parameters.provider.options)) {
      const options = parameters.provider.options[provider];
      if (!object(options)) throw new Error("Provider options must be an object");
      for (const key of Object.keys(options)) {
        if (!(capability.allowed_passthrough_parameters || []).includes(key)) throw new Error("Unsupported provider option: " + key);
        // These passthrough aliases can change billed inputs, duration,
        // dimensions or output count outside the normalized order facts.
        // Require the canonical top-level/metadata form instead.
        if (["images", "image", "last_image", "video", "videos", "audio", "size", "ratio", "aspectRatio", "keyframes", "version", "return_last_frame"].includes(key))
          throw new Error("Provider option changes billed inputs or output parameters; use canonical video parameters: " + key);
      }
    }
    body.provider = parameters.provider;
  }
  return { body: body, images: imageCount, videos: videoCount, audios: audioCount };
}

function validateReference(reference, frame) {
  if (!object(reference) || !["image_url", "audio_url", "video_url"].includes(reference.type) || (frame && reference.type !== "image_url"))
    throw new Error("Invalid input reference type");
  const part = reference[reference.type];
  const url = object(part) ? part.url : undefined;
  if (object(url) && url.__fileRef && reference.type === "image_url") return;
  if (typeof url !== "string" || !/^(https?:\/\/|data:(image|audio|video)\/)/.test(url)) throw new Error("Input references must contain a media URL");
}

export function buildSubmitRequest(ctx) {
  const order = videoOrder(ctx);
  return { url: ctx.baseUrl.replace(/\/$/, "") + "/v1/videos", method: "POST", headers: { Authorization: "Bearer " + ctx.apiKey, "Content-Type": "application/json" }, body: order.body };
}

export function extractUsage(ctx) {
  const order = videoOrder(ctx);
  // Zero means duration omitted, not a measured zero-second result. The
  // administrator must price the default branch or use a per-job tariff.
  return { requested_seconds: order.body.duration || 0, resolution: order.body.resolution || "default", size: order.body.size || "default", aspect_ratio: order.body.aspect_ratio || "default", upscale_factor: order.body.upscale_factor || 0, creativity: order.body.creativity === undefined ? "default" : String(order.body.creativity), audio: order.body.generate_audio === undefined ? "default" : order.body.generate_audio ? "enabled" : "disabled", images: order.images, videos: order.videos, audios: order.audios, jobs: 1 };
}

export function parseSubmitResponse(ctx, response) {
  const body = response.body;
  if (!object(body) || typeof body.id !== "string" || !body.id) throw new Error("OpenRouter did not return a video job ID; submission requires reconciliation");
  return { taskId: body.id, taskData: { id: body.id, status: body.status }, state: { catalogFetchedAt: catalog.fetched_at } };
}

export function buildQueryRequest(ctx) {
  return { url: ctx.baseUrl.replace(/\/$/, "") + "/v1/videos/" + encodeURIComponent(ctx.taskId), method: "GET", headers: { Authorization: "Bearer " + ctx.apiKey } };
}

export function parseTaskResult(ctx, body) {
  if (!object(body) || body.id !== ctx.taskId) return { status: "UNKNOWN", reason: "OpenRouter returned an invalid job identity" };
  const statuses = { pending: "QUEUED", in_progress: "IN_PROGRESS", completed: "SUCCESS", failed: "FAILURE", cancelled: "FAILURE", expired: "FAILURE" };
  const status = statuses[body.status];
  if (!status) return { status: "UNKNOWN", reason: "OpenRouter returned an unknown video state" };
  // Provider errors may contain URLs, request data or credentials.
  // Expose only a stable generic reason; the host stores purchase evidence.
  return { status: status, reason: status === "FAILURE" ? "OpenRouter video generation " + body.status : "" };
}

export function extractUsageOnComplete() {
  // OpenRouter only documents cost/is_byok. Keep the frozen order tariff;
  // never invent measured seconds, tokens, or a zero-cost completion.
  return {};
}

export function listArtifacts(task) {
  return task.status === "SUCCESS" ? [{ key: "video", type: "video", mimeType: "video/mp4" }] : [];
}

export function buildContentRequest(ctx) {
  if (ctx.artifactKey !== "video") throw new Error("artifact_not_found");
  // Reconstruct the documented same-origin URL. Do not trust unsigned_urls
  // or polling_url as credential destinations.
  return { url: ctx.baseUrl.replace(/\/$/, "") + "/v1/videos/" + encodeURIComponent(ctx.upstreamTaskId) + "/content?index=0", method: "GET", headers: { Authorization: "Bearer " + ctx.apiKey } };
}

export const protocols = {
  openai_video: {
    decodeRequest: function (ctx) {
      if (!ctx.body || !["json", "multipart"].includes(ctx.body.kind)) throw new Error("JSON or multipart body required");
      let request;
      if (ctx.body.kind === "json") {
        if (!object(ctx.body.value)) throw new Error("JSON object required");
        request = Object.assign({}, ctx.body.value);
      } else {
        request = {};
        for (const name of Object.keys(ctx.body.fields || {})) {
          const values = ctx.body.fields[name];
          if (values.length !== 1) throw new Error(name + " must be provided once");
          request[name] = values[0];
        }
        if (request.metadata !== undefined) {
          try { request.metadata = JSON.parse(request.metadata); } catch (_) { throw new Error("metadata must be a JSON object string"); }
        }
      }
      if (request.metadata !== undefined && !object(request.metadata)) throw new Error("metadata must be an object");
      request.model = ctx.model;
      return { kind: "submit", model: ctx.model, action: request.image || request.input_reference || (ctx.body.files || []).length || request.frame_images ? "image_to_video" : "text_to_video", requestBody: request };
    },
    render: function () { return {}; },
  },
};
