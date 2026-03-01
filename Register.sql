CREATE TABLE IF NOT EXISTS "Student" (
	"student_id" INTEGER NOT NULL UNIQUE,
	"first_name" VARCHAR(255) NOT NULL,
	"last_name" VARCHAR(255) NOT NULL,
	"email" VARCHAR(255) NOT NULL,
	"birthdate" VARCHAR(255) NOT NULL,
	"gender" VARCHAR(255) NOT NULL,
	"year_level" INTEGER NOT NULL,
	"graded_subject" VARCHAR(255) ARRAY,
	PRIMARY KEY("student_id")
);




CREATE TABLE IF NOT EXISTS "Course" (
	"course_id" INTEGER NOT NULL UNIQUE,
	"subject" VARCHAR(255) NOT NULL,
	"credit" INTEGER NOT NULL,
	"section" VARCHAR(255) ARRAY NOT NULL,
	"day_of_week" VARCHAR(255) NOT NULL,
	"start_time" TIME NOT NULL,
	"end_time" TIME NOT NULL,
	"capacity" INTEGER NOT NULL,
	"state" VARCHAR(255) NOT NULL,
	"prerequisite" VARCHAR(255),
	PRIMARY KEY("course_id")
);




CREATE TABLE IF NOT EXISTS "student_course" (
	"student_id" INTEGER NOT NULL,
	"course_id" INTEGER NOT NULL
);



ALTER TABLE "student_course"
ADD FOREIGN KEY("student_id") REFERENCES "Student"("student_id")
ON UPDATE NO ACTION ON DELETE NO ACTION;
ALTER TABLE "student_course"
ADD FOREIGN KEY("course_id") REFERENCES "Course"("course_id")
ON UPDATE NO ACTION ON DELETE NO ACTION;